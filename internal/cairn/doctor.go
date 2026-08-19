package cairn

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Finding is doctor's unified report envelope. Every check -- whether it
// adapts an existing typed finding (DedupFinding, SweepFinding) or is
// doctor-native (malformed file, duplicate ID, scope mismatch, invalid tag,
// index drift) -- reports through this one shape, so a --json consumer has a
// single type to walk instead of five differently-shaped structs.
type Finding struct {
	Category string         `json:"category"`
	Severity string         `json:"severity"`
	Message  string         `json:"message"`
	EntryIDs []string       `json:"entry_ids,omitempty"`
	Detail   map[string]any `json:"detail,omitempty"`
}

// Finding categories.
const (
	CategoryMalformed     = "malformed_entry"
	CategoryDuplicateID   = "duplicate_id"
	CategoryScopeMismatch = "scope_mismatch"
	CategoryInvalidTag    = "invalid_tag"
	CategoryDedup         = "dedup"
	CategoryFreshness     = "freshness"
	CategoryIndexDrift    = "index_drift"
)

// Finding severities.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Report is Diagnose's aggregate result. ExitCode mirrors FR-9's 0/1
// convention (0: no findings, 1: findings present) so a --json consumer
// doesn't have to recompute len(Findings) itself; the third convention
// value, 2 ("operational failure"), can never appear here, since Diagnose
// returns no Report at all when it hits that case -- it returns an error
// instead.
type Report struct {
	Store    string     `json:"store"`
	Findings []Finding  `json:"findings"`
	ExitCode int        `json:"exit_code"`
	Stats    StoreStats `json:"stats"`
}

// StoreStats is Diagnose's summary of the store shape, derived from the same
// tolerant entry walk Diagnose already performs -- no second store walk.
// EntriesByTier groups the valid (parsed) entries by their on-disk tier
// (entryTier's classification, e.g. "global", "rig", "role", "agent").
// IndexDrift mirrors the same IndexStale(ctx, store) result that produces
// the index_drift Finding, reused here rather than queried twice.
type StoreStats struct {
	EntriesByTier map[string]int `json:"entries_by_tier"`
	IndexDrift    bool           `json:"index_drift"`
}

// Diagnose runs every check doctor knows about over one tolerant walk of
// store and returns the aggregate Report. Every parse failure the walk
// collects becomes exactly one malformed_entry finding (FR-3/NFR-1); every
// other check runs only over the entries that did parse, so one corrupted
// file never blocks the rest of the report. The returned error is non-nil
// only for a genuine I/O failure reading the store root itself (surfaced by
// IterEntriesTolerant) -- never for a finding, however severe. That
// distinction is what lets the command layer implement FR-9's exit code
// split: an error here means "could not complete" (exit 2); a returned
// Report, however many findings it carries, means the run itself completed
// (exit 0 or 1).
func Diagnose(ctx context.Context, store string) (*Report, error) {
	return diagnose(ctx, store, IterEntriesTolerant, IndexStale)
}

// diagnose is Diagnose's injectable core: iterEntriesTolerant and
// indexStaleFn default to IterEntriesTolerant and IndexStale in Diagnose,
// and are swapped for call-counting wrappers in tests asserting each runs
// exactly once (mirroring ensureFresh/ensureFreshWith in index.go).
func diagnose(
	ctx context.Context,
	store string,
	iterEntriesTolerant func(context.Context, string) ([]*Entry, []ParseFailure, error),
	indexStaleFn func(context.Context, string) (bool, error),
) (*Report, error) {
	entries, failures, err := iterEntriesTolerant(ctx, store)
	if err != nil {
		return nil, err
	}

	var findings []Finding
	for _, f := range failures {
		findings = append(findings, fromParseFailure(f))
	}
	findings = append(findings, DuplicateIDs(entries)...)
	findings = append(findings, scopeMismatch(store, entries)...)
	findings = append(findings, invalidTags(entries)...)

	if dedupFindings, derr := DedupEntries(store, entries); derr != nil {
		findings = append(findings, Finding{
			Category: CategoryDedup,
			Severity: SeverityError,
			Message:  fmt.Sprintf("dedup check could not complete: %s", derr),
		})
	} else {
		for _, f := range dedupFindings {
			findings = append(findings, fromDedupFinding(f))
		}
	}

	if sweepFindings, serr := SweepEntries(ctx, store, entries); serr != nil {
		findings = append(findings, Finding{
			Category: CategoryFreshness,
			Severity: SeverityError,
			Message:  fmt.Sprintf("freshness check could not complete: %s", serr),
		})
	} else {
		for _, f := range sweepFindings {
			if adapted := fromSweepFinding(f); adapted != nil {
				findings = append(findings, *adapted)
			}
		}
	}

	findings = append(findings, agentFreshness(ctx, store, entries)...)
	driftFindings, stale := indexDrift(ctx, store, entries, indexStaleFn)
	findings = append(findings, driftFindings...)

	report := &Report{
		Store:    store,
		Findings: findings,
		Stats: StoreStats{
			EntriesByTier: tierCounts(store, entries),
			IndexDrift:    stale,
		},
	}
	if len(findings) > 0 {
		report.ExitCode = 1
	}
	return report, nil
}

// tierCounts groups entries by their on-disk tier (entryTier's
// classification) for Report.Stats.EntriesByTier.
func tierCounts(store string, entries []*Entry) map[string]int {
	counts := make(map[string]int)
	for _, e := range entries {
		counts[entryTier(store, e)]++
	}
	return counts
}

// fromParseFailure adapts one ParseFailure into a malformed_entry Finding.
// Only the parse error's message is included, never the offending file's
// content: a corrupted body could itself be mid-edit sensitive draft text.
func fromParseFailure(f ParseFailure) Finding {
	return Finding{
		Category: CategoryMalformed,
		Severity: SeverityError,
		Message:  fmt.Sprintf("%s: failed to parse: %s", f.Path, f.Err),
		Detail:   map[string]any{"path": f.Path},
	}
}

// fromDedupFinding adapts one DedupFinding into the unified envelope,
// re-shaping the wrapper only -- the underlying dedup logic (topic_key
// collision or content similarity) is entirely DedupEntries', unchanged.
func fromDedupFinding(f DedupFinding) Finding {
	detail := map[string]any{"kind": f.Kind}
	if f.Tier != "" {
		detail["tier"] = f.Tier
	}
	if f.TopicKey != "" {
		detail["topic_key"] = f.TopicKey
	}
	if f.Similarity != 0 {
		detail["similarity"] = f.Similarity
	}
	return Finding{
		Category: CategoryDedup,
		Severity: SeverityWarning,
		Message:  f.Detail,
		EntryIDs: f.EntryIDs,
		Detail:   detail,
	}
}

// fromSweepFinding adapts one SweepFinding into the unified envelope. Only
// non-Fresh statuses become a Finding: a clean store's healthy anchors must
// never accumulate as noise, or FR-9's exit-0 "no findings" case could never
// be reached even on a store doctor's other checks consider perfectly
// healthy.
func fromSweepFinding(f SweepFinding) *Finding {
	if f.Status == Fresh {
		return nil
	}
	return &Finding{
		Category: CategoryFreshness,
		Severity: SeverityWarning,
		Message:  fmt.Sprintf("entry %q anchor is %s: %s", f.ID, f.Status, f.Detail),
		EntryIDs: []string{f.ID},
		Detail:   map[string]any{"tier": f.Tier, "status": f.Status, "anchor_type": f.AnchorType},
	}
}

// agentFreshness runs Check directly over just the agent-tier entries -- the
// one tier SweepEntries structurally excludes ("librarian's remit", see
// sweep.go), so this is the narrow second pass FR-8 needs for full-store
// anchor coverage, not a redundant reimplementation of Sweep for the tiers
// it already covers.
func agentFreshness(ctx context.Context, store string, entries []*Entry) []Finding {
	var out []Finding
	for _, e := range entries {
		if entryTier(store, e) != "agent" {
			continue
		}
		status, detail := Check(ctx, e)
		if status == Fresh {
			continue
		}
		out = append(out, Finding{
			Category: CategoryFreshness,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("entry %q anchor is %s: %s", e.ID, status, detail),
			EntryIDs: []string{e.ID},
			Detail:   map[string]any{"tier": "agent", "status": status, "anchor_type": e.Anchor.Type},
		})
	}
	return out
}

// indexDrift reports whether the on-disk index is behind the store's
// current HEAD (indexStaleFn), and separately confirms the tolerantly-valid
// entry set reindexes cleanly (ReindexEntries) -- the second signal doctor
// gets that a plain staleness check can't provide: whether a rebuild would
// actually succeed, not just whether one is due. Calling ReindexEntries
// here does rebuild the on-disk index (it is the same upsert/transaction
// Reindex itself runs) -- but index.sqlite is a rebuildable, gitignored
// materialized view, not store content, so refreshing it is the same
// incidental side effect every other read command's ensureFresh already has;
// doctor takes no write path against the entries themselves (NFR-3).
// indexStaleFn is injected (defaulting to IndexStale via diagnose) and its
// result is returned alongside the findings, so a caller populating both the
// index_drift Finding and Report.Stats.IndexDrift can do so from this one
// call instead of checking staleness twice.
func indexDrift(
	ctx context.Context,
	store string,
	entries []*Entry,
	indexStaleFn func(context.Context, string) (bool, error),
) ([]Finding, bool) {
	var out []Finding

	stale, err := indexStaleFn(ctx, store)
	switch {
	case err != nil:
		out = append(out, Finding{
			Category: CategoryIndexDrift,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("could not determine index staleness: %s", err),
			Detail:   map[string]any{"error": err.Error()},
		})
	case stale:
		out = append(out, Finding{
			Category: CategoryIndexDrift,
			Severity: SeverityWarning,
			Message:  "index is stale: indexed watermark does not match the store's current HEAD",
			Detail:   map[string]any{"stale": true},
		})
	}

	if _, err := ReindexEntries(ctx, store, entries); err != nil {
		out = append(out, Finding{
			Category: CategoryIndexDrift,
			Severity: SeverityError,
			Message:  fmt.Sprintf("index rebuild failed on the valid entry set: %s", err),
			Detail:   map[string]any{"error": err.Error()},
		})
	}

	return out, stale
}

// DuplicateIDs reports every entry ID held by more than one file (FR-4).
// reindexUpsertChunkTx's upsert (INSERT ... ON CONFLICT(id) DO UPDATE)
// silently drops a collision today -- this is the only place that surfaces
// it as a finding.
func DuplicateIDs(entries []*Entry) []Finding {
	byID := make(map[string][]*Entry)
	for _, e := range entries {
		byID[e.ID] = append(byID[e.ID], e)
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var out []Finding
	for _, id := range ids {
		group := byID[id]
		if len(group) < 2 {
			continue
		}
		paths := make([]string, len(group))
		for i, e := range group {
			paths[i] = e.BodyPath
		}
		out = append(out, Finding{
			Category: CategoryDuplicateID,
			Severity: SeverityError,
			Message:  fmt.Sprintf("entry id %q is used by %d files; only one survives in the index", id, len(group)),
			EntryIDs: []string{id},
			Detail:   map[string]any{"paths": paths},
		})
	}
	return out
}

// scopeMismatch reports entries whose scope tags don't name the entry's own
// on-disk directory (FR-5). tier comes from entryTier, the same disk-derived
// classification Sweep uses; global/ entries are exempt -- DESIGN.md §3
// permits a global-filed entry to carry any (or no) scope tags, since
// "global" isn't itself a taggable dimension the way rig/role/agent are.
// This is deliberately not a tag-shape/safety check -- that's invalidTags'
// (FR-6) job; conflating the two would make FR-5 and FR-6 the same finding
// under two names (OQ1).
func scopeMismatch(store string, entries []*Entry) []Finding {
	var out []Finding
	for _, e := range entries {
		tier := entryTier(store, e)
		if tier == "" || tier == "global" {
			continue
		}
		name := scopeDirName(store, e)
		if name == "" {
			continue
		}
		want := tier + ":" + name
		if hasTag(e.Scope, want) {
			continue
		}
		out = append(out, Finding{
			Category: CategoryScopeMismatch,
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("entry %q is filed under %s/ but its scope tags do not include %q", e.ID, filepath.Join(tier, name), want),
			EntryIDs: []string{e.ID},
			Detail:   map[string]any{"disk_tier": tier, "expected_tag": want, "scope": e.Scope},
		})
	}
	return out
}

// scopeDirName returns the directory segment immediately under the tier
// directory in e's on-disk path (e.g. "alpha" for rig/alpha/foo.md), or ""
// if e.BodyPath isn't shaped that way. entryTier already derives the tier
// itself (the same relative path's first segment) but every other caller of
// it (Sweep, Dedup) only ever needs that first segment, not this second one
// -- kept separate rather than changing entryTier's signature for its
// already-covered existing callers.
func scopeDirName(store string, e *Entry) string {
	rel, err := filepath.Rel(store, e.BodyPath)
	if err != nil {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// invalidTags reports every scope tag that fails ValidateScopeTag: tier:value
// with tier in {rig,role,agent} and value passing ValidatePathSegment.
// DESIGN.md §2's global tier is expressed by an *empty* Scope, never by a
// tag naming it, so a tier-less tag -- including the literal word "global"
// -- is invalid here too. ValidateScopeTag today only runs at remember-time
// (cmd/remember.go), so it never re-validates a body file that reached disk
// another way -- hand-edited, git merged, or written by an older cairn
// version; this check re-applies it store-wide.
func invalidTags(entries []*Entry) []Finding {
	var out []Finding
	for _, e := range entries {
		for _, tag := range e.Scope {
			if reason := tagInvalidReason(tag); reason != "" {
				out = append(out, Finding{
					Category: CategoryInvalidTag,
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("entry %q has an invalid scope tag %q: %s", e.ID, tag, reason),
					EntryIDs: []string{e.ID},
					Detail:   map[string]any{"tag": tag, "reason": reason},
				})
			}
		}
	}
	return out
}

// tagInvalidReason returns "" for a valid tag, else a human-readable reason
// it fails ValidateScopeTag.
func tagInvalidReason(tag string) string {
	if err := ValidateScopeTag(tag); err != nil {
		return err.Error()
	}
	return ""
}

// ExplainResult is ExplainEntry's store-wide answer (FR-7): whether entryID
// is shadowed by another entry sharing its topic_key, and if so, by which
// entry and by which moreSpecific tiebreak rule.
type ExplainResult struct {
	EntryID      string `json:"entry_id"`
	TopicKey     string `json:"topic_key,omitempty"`
	ShadowedByID string `json:"shadowed_by_id,omitempty"`
	Rule         string `json:"rule,omitempty"`
}

// ExplainEntry answers FR-7's store-wide question: "if any identity that can
// see this entry's scope is looking, is it shadowed, by what, and by which
// rule?" It locates entryID's topic_key group across the whole store and
// delegates to bestShadowerExplain -- the same reason-carrying resolver
// ShadowMap itself uses (identity-free by construction), so this is a
// second consumer of that logic, not a re-derivation (NFR-2). ctx is unused
// today but kept for signature consistency with Diagnose/ExplainForIdentity
// (the design doc's sequence diagrams specify ExplainEntry(ctx, store, id)).
//
//nolint:revive // see doc comment: deliberate signature-consistency param
func ExplainEntry(ctx context.Context, store, entryID string) (*ExplainResult, error) {
	entries, _, err := IterEntriesTolerant(ctx, store)
	if err != nil {
		return nil, err
	}

	var target *Entry
	for _, e := range entries {
		if e.ID == entryID {
			target = e
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("no entry with id %q", entryID)
	}

	res := &ExplainResult{EntryID: entryID, TopicKey: target.TopicKey}
	if target.TopicKey == "" {
		return res, nil
	}

	var group []*Entry
	for _, e := range entries {
		if e.TopicKey == target.TopicKey {
			group = append(group, e)
		}
	}

	shadowedBy, rule := bestShadowerExplain(target, group)
	if shadowedBy != nil {
		res.ShadowedByID = shadowedBy.ID
		res.Rule = rule
	}
	return res, nil
}

// IdentityCandidate is one topic-key candidate's satisfaction verdict
// against a queried identity, part of ExplainForIdentity's result.
type IdentityCandidate struct {
	EntryID   string   `json:"entry_id"`
	Scope     []string `json:"scope"`
	Satisfied bool     `json:"satisfied"`
}

// IdentityExplainResult is ExplainForIdentity's answer (FR-7, identity-scoped
// mode): which of topicKey's candidates the identity's tags actually satisfy,
// and among the satisfying ones, which wins and by which rule.
type IdentityExplainResult struct {
	TopicKey   string              `json:"topic_key"`
	Candidates []IdentityCandidate `json:"candidates"`
	WinnerID   string              `json:"winner_id,omitempty"`
	Rule       string              `json:"rule,omitempty"`
}

// ExplainForIdentity answers FR-7's identity-scoped question: "given my
// identity, why did my recall resolve to X and not Y?" This is not
// answerable by ExplainEntry/bestShadowerExplain -- Visible's shadow() is
// the function that actually runs per-identity, using a different rule
// (tag-count specificity, sound only over one identity's pre-filtered
// candidates, per ShadowMap's own doc comment) -- so this reports each
// candidate's plain Scope-subset satisfaction first (useful on its own when
// the answer is "none of them, that's why you got nothing back"), then runs
// shadowReason, shadow's reason-carrying sibling, over the satisfying subset
// only. ctx is unused today but kept for signature consistency with
// Diagnose/ExplainEntry (the design doc's sequence diagrams specify
// ExplainForIdentity(ctx, store, identity, topicKey)).
//
//nolint:revive // see doc comment: deliberate signature-consistency param
func ExplainForIdentity(ctx context.Context, store string, identity []string, topicKey string) (*IdentityExplainResult, error) {
	entries, _, err := IterEntriesTolerant(ctx, store)
	if err != nil {
		return nil, err
	}

	var group []*Entry
	for _, e := range entries {
		if e.TopicKey == topicKey {
			group = append(group, e)
		}
	}

	res := &IdentityExplainResult{TopicKey: topicKey}
	var satisfying []*Entry
	for _, e := range group {
		ok := identitySatisfies(identity, e.Scope)
		res.Candidates = append(res.Candidates, IdentityCandidate{
			EntryID:   e.ID,
			Scope:     e.Scope,
			Satisfied: ok,
		})
		if ok {
			satisfying = append(satisfying, e)
		}
	}

	_, reasons := shadowReason(satisfying)
	for _, r := range reasons {
		if r.TopicKey == topicKey {
			res.WinnerID = r.WinnerID
			res.Rule = r.Rule
			return res, nil
		}
	}
	// shadowReason produces no ShadowReason for an uncontested key (a
	// topic_key held by a single candidate, per its own doc comment), but a
	// lone satisfying candidate is still the de facto winner here.
	if len(satisfying) == 1 {
		res.WinnerID = satisfying[0].ID
	}
	return res, nil
}

// identitySatisfies reports whether every tag in scope is present in
// identity -- the same subset test visibleFrom applies inline while
// filtering a whole store, exposed here as its own function so
// ExplainForIdentity can report per-candidate satisfaction instead of just
// a final filtered list.
func identitySatisfies(identity, scope []string) bool {
	idset := make(map[string]struct{}, len(identity))
	for _, t := range identity {
		idset[t] = struct{}{}
	}
	for _, tag := range scope {
		if _, ok := idset[tag]; !ok {
			return false
		}
	}
	return true
}
