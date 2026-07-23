package cairn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// ReviewMergeBranch describes one pending remember/* branch: a shared-tier
// entry a contributor has written but that hasn't yet been curated and
// merged by a reviewer (DESIGN.md §7, "shared = branch, merge request,
// review, merge"). Named distinctly from branches.go's ReviewBranch (the
// librarian sweep's stale-branch bookkeeping type, crn-0yv.1): the two model
// overlapping but different concerns -- this one is shaped for the
// interactive list/show/merge review flow, that one for age/SHA-tracked
// notify-escalate state -- and were developed independently before either
// saw the other (crn-j1uh).
type ReviewMergeBranch struct {
	Name      string // e.g. "remember/foo-a1b2c3d4"
	Tier      string // "global" | "rig" | "role" -- never "agent" (crn-xw3 FR-8: agent/ is private and never reaches a review branch)
	TierValue string // e.g. "web" for a rig:web entry; "" for global
	EntryPath string // the changed entry file's path, relative to store
}

// DefaultBranch resolves the store's currently checked-out branch -- the
// merge target for ListReviewMergeBranches/ShowReviewBranch's diff base and
// MergeReviewBranch's actual merge destination.
func DefaultBranch(ctx context.Context, store string) (string, error) {
	out, err := gitRun(ctx, store, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve default branch (is %s a git repo with HEAD on a branch?): %w", store, err)
	}
	return strings.TrimSpace(out), nil
}

// ListReviewMergeBranches discovers local remember/* branches and, for each,
// derives its tier from the single entry file it changes -- never from the
// branch name -- per crn-xw3 AF1: the branch is only ever named after the
// entry's random id (reviewBranchName), which carries no tier information.
func ListReviewMergeBranches(ctx context.Context, store string) ([]ReviewMergeBranch, error) {
	def, err := DefaultBranch(ctx, store)
	if err != nil {
		return nil, err
	}
	out, err := gitRun(ctx, store, "for-each-ref", "--format=%(refname:short)", "refs/heads/remember/")
	if err != nil {
		return nil, fmt.Errorf("list remember/* branches: %w", err)
	}

	var branches []ReviewMergeBranch
	for _, name := range strings.Split(out, "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		relPath, err := changedEntryFile(ctx, store, def, name)
		if err != nil {
			return nil, err
		}
		tier, value, err := tierFromEntryPath(relPath)
		if err != nil {
			return nil, fmt.Errorf("branch %q: %w", name, err)
		}
		branches = append(branches, ReviewMergeBranch{Name: name, Tier: tier, TierValue: value, EntryPath: relPath})
	}
	return branches, nil
}

// changedEntryFile returns the single entry file branch changes relative to
// defaultBr. Every remember/* branch commits exactly one entry
// (CommitToReviewBranch), so anything other than exactly one changed file
// means the branch isn't in the shape list/show/merge expect.
func changedEntryFile(ctx context.Context, store, defaultBr, branch string) (string, error) {
	out, err := gitRun(ctx, store, "diff", "--name-only", defaultBr+"..."+branch)
	if err != nil {
		return "", fmt.Errorf("diff %s against %s: %w", branch, defaultBr, err)
	}
	var files []string
	for _, ln := range strings.Split(out, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			files = append(files, ln)
		}
	}
	if len(files) != 1 {
		return "", fmt.Errorf("review branch %q changes %d files (expected exactly 1 entry file): %v", branch, len(files), files)
	}
	return files[0], nil
}

// tierFromEntryPath derives a DESIGN.md §2 tier from an entry file's
// store-relative path -- global/..., rig/<value>/..., or role/<value>/...
// (matching scopeDirs in entry.go). agent/ is the private tier and never
// reaches a review branch (CommitDirect, not CommitToReviewBranch, handles
// it), so it's reported as an error here rather than a fourth valid tier.
func tierFromEntryPath(relPath string) (tier, value string, err error) {
	parts := strings.SplitN(filepath.ToSlash(relPath), "/", 3)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("entry path %q too short to derive a tier", relPath)
	}
	switch parts[0] {
	case "global":
		return "global", "", nil
	case "rig", "role":
		return parts[0], parts[1], nil
	case "agent":
		return "", "", fmt.Errorf("entry path %q resolves to the private agent/ tier, which never reaches a review branch", relPath)
	default:
		return "", "", fmt.Errorf("entry path %q has unrecognized top-level scope dir %q", relPath, parts[0])
	}
}

// ShowReviewBranch returns branch's full diff against the default branch,
// plus its entry parsed from the branch tip -- not the working tree, since a
// remember/* branch is never checked out anywhere (CommitToReviewBranch
// commits it via a throwaway worktree that's removed immediately after).
func ShowReviewBranch(ctx context.Context, store, branch string) (diff string, entry *Entry, err error) {
	def, err := DefaultBranch(ctx, store)
	if err != nil {
		return "", nil, err
	}
	relPath, err := changedEntryFile(ctx, store, def, branch)
	if err != nil {
		return "", nil, err
	}
	diff, err = gitRun(ctx, store, "diff", def+"..."+branch)
	if err != nil {
		return "", nil, fmt.Errorf("diff %s against %s: %w", branch, def, err)
	}
	raw, err := gitRun(ctx, store, "show", branch+":"+relPath)
	if err != nil {
		return "", nil, fmt.Errorf("read %s from %s: %w", relPath, branch, err)
	}
	e, err := parseEntryContent([]byte(raw), branch+":"+relPath)
	if err != nil {
		return "", nil, err
	}
	e.BodyPath = relPath
	return diff, e, nil
}

// splitFrontmatterForPatch splits +++-fenced content into the frontmatter
// text (front, excluding both fences) and everything from the closing fence
// onward (closeAndBody), mirroring ParseEntry's own fence-parsing rules
// exactly. Named distinctly from entry.go's splitFrontmatter (same fence
// grammar, different contract: that one reads a path via os.ReadFile and
// returns body without the closing fence; this one takes already-read
// bytes -- review branches are never checked out to a path ParseEntry could
// read from directly, their content only ever exists as `git show` output
// -- and keeps the closing fence in closeAndBody so patchFrontmatterFields
// can reassemble the file byte-exact around a surgical field edit.
func splitFrontmatterForPatch(raw []byte) (front, closeAndBody string, err error) {
	text := string(raw)
	if !strings.HasPrefix(text, fence) {
		return "", "", errNotEntry
	}
	rest := text[len(fence):]
	end := strings.Index(rest, "\n"+fence)
	if end < 0 {
		return "", "", fmt.Errorf("unterminated %s frontmatter", fence)
	}
	return rest[:end], rest[end:], nil
}

// parseEntryContent decodes an entry from already-read frontmatter+body
// bytes. source labels errors (e.g. "branch:path", which has no filesystem
// path ParseEntry could use instead).
func parseEntryContent(raw []byte, source string) (*Entry, error) {
	front, closeAndBody, err := splitFrontmatterForPatch(raw)
	if err != nil {
		if errors.Is(err, errNotEntry) {
			return nil, err
		}
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	var e Entry
	if _, err := toml.Decode(front, &e); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	if e.ID == "" {
		return nil, errNotEntry
	}
	e.Body = strings.TrimLeft(closeAndBody[len("\n"+fence):], "\n")
	return &e, nil
}

// secretPatterns are high-confidence, well-known credential formats.
// MergeReviewBranch's guard is a best-effort backstop, not a substitute for
// the reviewer's own judgment via `cairn review show` -- crn-xw3's
// architecture doc explicitly rejects relying on automated detection alone
// ("there is no mechanical way to... reliably catch every secret pattern --
// that judgment is the point of having a reviewer at all"). Deliberately
// narrow, named-format regexes only, never fuzzy keyword matching, so a
// legitimate entry that's *about* secrets handling doesn't false-positive.
var secretPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"AWS access key ID", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"GitHub token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`)},
	{"Slack token", regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},
	{"Google API key", regexp.MustCompile(`AIza[0-9A-Za-z_\-]{35}`)},
	{"Stripe live secret key", regexp.MustCompile(`sk_live_[0-9a-zA-Z]{20,}`)},
	{"private key block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
}

// detectSecretPattern returns the name of the first obvious secret-like
// pattern found in text, or "" if none match.
func detectSecretPattern(text string) string {
	for _, p := range secretPatterns {
		if p.re.MatchString(text) {
			return p.name
		}
	}
	return ""
}

// ReviewMergeOptions carries what a reviewer supplies at merge time. TopicKey
// is required: DESIGN.md §6 assigns the canonical topic_key at merge time,
// not from the contributor's `remember --topic` hint. AnchorType, Scope, and
// Kind are optional -- omitted, they leave those fields as the contributor
// wrote them. AutoActionable is a one-way grant (mirroring AllowSecretPattern
// -- there is no flag to revoke it here); MergeReviewBranch rejects it unless
// the effective kind (Kind if given, else the entry's existing kind) is
// "remediation". Bead is an optional traceability reference for the merge
// commit's "(<bead-id>)" suffix; if omitted, the (possibly just-curated)
// TopicKey is used instead.
type ReviewMergeOptions struct {
	TopicKey           string
	AnchorType         string
	Scope              []string
	Kind               string // "" (leave as contributor wrote it) | "remediation" | "note"
	AutoActionable     bool   // requires effective Kind == "remediation"; reviewer-granted, never self-declared
	Bead               string
	AllowSecretPattern bool
}

// ReviewMergeResult reports the outcome of a successful merge.
type ReviewMergeResult struct {
	SHA string
}

// MergeReviewBranch curates and merges branch into the store's default
// branch: patch the reviewer-assigned frontmatter fields, guard against an
// obvious unreviewed secret, merge --no-ff, delete the branch, and reindex.
// Per NFR-2 (no partial state on failure): a patch failure never touches the
// default branch at all; a merge failure runs `git merge --abort` so the
// default branch is left exactly as it was; but once the merge itself
// succeeds it is durable and irreversible, so a subsequent branch-delete or
// reindex failure is reported as a distinct partial-success error rather
// than rolled back.
func MergeReviewBranch(ctx context.Context, store, branch string, opts ReviewMergeOptions) (*ReviewMergeResult, error) {
	if err := ValidatePathSegment(opts.TopicKey); err != nil {
		return nil, fmt.Errorf("invalid --topic-key: %w", err)
	}
	for _, tag := range opts.Scope {
		if err := ValidatePathSegment(tag); err != nil {
			return nil, fmt.Errorf("invalid --scope tag %q: %w", tag, err)
		}
	}
	if opts.AnchorType != "" {
		if err := ValidatePathSegment(opts.AnchorType); err != nil {
			return nil, fmt.Errorf("invalid --anchor-type: %w", err)
		}
	}
	if opts.Kind != "" && opts.Kind != "remediation" && opts.Kind != "note" {
		return nil, fmt.Errorf("invalid --kind %q: must be \"remediation\" or \"note\"", opts.Kind)
	}

	def, relPath, entry, err := curateReviewBranch(ctx, store, branch, opts)
	if err != nil {
		return nil, err
	}
	return mergeCuratedBranch(ctx, store, def, branch, relPath, entry, opts)
}

// curateReviewBranch resolves branch's single changed entry file against
// the default branch, blocks on an apparent secret unless
// opts.AllowSecretPattern, and -- if opts requests any frontmatter changes
// -- commits a curation patch onto branch before it is merged. Returns the
// default branch name alongside so the caller doesn't re-resolve HEAD a
// second time.
func curateReviewBranch(ctx context.Context, store, branch string, opts ReviewMergeOptions) (def, relPath string, entry *Entry, err error) {
	def, err = DefaultBranch(ctx, store)
	if err != nil {
		return "", "", nil, err
	}
	relPath, err = changedEntryFile(ctx, store, def, branch)
	if err != nil {
		return "", "", nil, err
	}
	rawStr, err := gitRun(ctx, store, "show", branch+":"+relPath)
	if err != nil {
		return "", "", nil, fmt.Errorf("read %s from %s: %w", relPath, branch, err)
	}
	raw := []byte(rawStr)

	if name := detectSecretPattern(rawStr); name != "" && !opts.AllowSecretPattern {
		return "", "", nil, fmt.Errorf(
			"branch %q looks like it contains a %s -- inspect with 'cairn review show %s' and "+
				"re-run with --allow-secret-pattern only if this is a false positive",
			branch, name, branch)
	}

	entry, err = parseEntryContent(raw, branch+":"+relPath)
	if err != nil {
		return "", "", nil, fmt.Errorf("parse entry: %w", err)
	}

	if opts.AutoActionable {
		effectiveKind := opts.Kind
		if effectiveKind == "" {
			effectiveKind = entry.Kind
		}
		if effectiveKind != "remediation" {
			return "", "", nil, fmt.Errorf(
				"--auto-actionable requires an effective kind of \"remediation\" (got %q); "+
					"pass --kind remediation, or merge an entry whose existing kind is already remediation",
				effectiveKind)
		}
	}

	patched, err := patchFrontmatterFields(raw, opts)
	if err != nil {
		return "", "", nil, fmt.Errorf("patch frontmatter fields: %w", err)
	}
	if string(patched) != rawStr {
		if err := commitCurationPatch(ctx, store, branch, relPath, patched, opts.TopicKey); err != nil {
			return "", "", nil, err
		}
	}

	return def, relPath, entry, nil
}

// mergeCuratedBranch merges an already-curated branch into def and reindexes.
// It leaves the default branch untouched on any failure up through the merge
// call itself (NFR-2); branch-delete and reindex failures after a successful
// merge are reported as distinct partial-success errors alongside the result.
func mergeCuratedBranch(
	ctx context.Context, store, def, branch, relPath string, entry *Entry, opts ReviewMergeOptions,
) (*ReviewMergeResult, error) {
	ref := opts.Bead
	if ref == "" {
		ref = opts.TopicKey
	}
	msg := fmt.Sprintf("librarian: merge %s — %s (%s)", branch, entry.Title, ref)

	// Create left an untracked draft copy of relPath sitting in the store's
	// own working tree (deliberately -- so `cairn status` can see a pending
	// shared-tier entry before it's reviewed; see CommitToReviewBranch), and
	// that is the ordinary case here, not an edge case: cairn-store has no
	// remote, so the reviewer runs `review merge` against the very same
	// working tree the contributor's `remember` call wrote into. Left in
	// place, it makes `git merge` refuse with "untracked working tree files
	// would be overwritten." The merge is about to write the (possibly
	// curated) canonical version at this same path anyway, so clear it
	// first -- scoped to exactly relPath, so it can never remove anything
	// else untracked in the tree, and a no-op if there's nothing there.
	if _, err := gitRun(ctx, store, "clean", "-f", "-q", "--", relPath); err != nil {
		return nil, fmt.Errorf("clear draft copy of %s before merge: %w", relPath, err)
	}
	if _, err := gitRun(ctx, store, "merge", "--no-ff", branch, "-m", msg); err != nil {
		_, _ = gitRun(ctx, store, "merge", "--abort")
		return nil, fmt.Errorf("merge %s into %s (aborted -- default branch unchanged): %w", branch, def, err)
	}
	sha, err := gitRun(ctx, store, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("merge succeeded but could not resolve the resulting SHA: %w", err)
	}
	result := &ReviewMergeResult{SHA: strings.TrimSpace(sha)}

	if _, err := gitRun(ctx, store, "branch", "-d", branch); err != nil {
		return result, fmt.Errorf("merged at %s but failed to delete branch %q (delete manually): %w", result.SHA, branch, err)
	}
	if _, err := Reindex(ctx, store); err != nil {
		return result, fmt.Errorf("merged at %s and deleted %q, but reindex failed (run 'cairn reindex' manually): %w", result.SHA, branch, err)
	}
	return result, nil
}

// patchFrontmatterFields rewrites only the specific lines opts requests,
// leaving every other line -- comments, key order, unrelated fields -- byte
// for byte untouched. This is required over Entry.WriteBack's full
// toml.NewEncoder re-encode, which reformats the entire frontmatter and so
// produces a full-file diff for a one-field curation change (crn-6az.5.1).
func patchFrontmatterFields(raw []byte, opts ReviewMergeOptions) ([]byte, error) {
	front, closeAndBody, err := splitFrontmatterForPatch(raw)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(front, "\n")

	lines = setScalarLine(lines, "topic_key", tomlQuote(opts.TopicKey))
	if opts.Scope != nil {
		lines = setScalarLine(lines, "scope", tomlArray(opts.Scope))
	}
	if opts.AnchorType != "" {
		lines, err = setAnchorTypeLine(lines, tomlQuote(opts.AnchorType))
		if err != nil {
			return nil, err
		}
	}
	if opts.Kind != "" {
		lines = setScalarLine(lines, "kind", tomlQuote(opts.Kind))
	}
	if opts.AutoActionable {
		lines = setScalarLine(lines, "auto_actionable", "true")
	}

	return []byte(fence + strings.Join(lines, "\n") + closeAndBody), nil
}

// setScalarLine replaces a top-level "key = ..." line (search stops at the
// first "[table]" header, so a same-named key nested under a table -- e.g.
// [anchor]'s own "type" -- is never touched), or inserts one right after
// "id = ..." if key isn't already present. id is always present and always
// first: Entry.ID has no omitempty tag and is the struct's first field.
func setScalarLine(lines []string, key, value string) []string {
	newLine := key + " = " + value
	for i, ln := range lines {
		if strings.HasPrefix(ln, "[") {
			break
		}
		if k, _, ok := strings.Cut(ln, "="); ok && strings.TrimSpace(k) == key {
			return spliceLine(lines, i, newLine)
		}
	}
	for i, ln := range lines {
		if k, _, ok := strings.Cut(ln, "="); ok && strings.TrimSpace(k) == "id" {
			return insertAfter(lines, i, newLine)
		}
	}
	return append([]string{newLine}, lines...)
}

// setAnchorTypeLine replaces the "type = ..." line nested under the
// [anchor] table specifically -- not Entry's own top-level "type" field,
// which shares the same key name.
func setAnchorTypeLine(lines []string, value string) ([]string, error) {
	newLine := "type = " + value
	inAnchor := false
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "[") {
			inAnchor = trimmed == "[anchor]"
			continue
		}
		if !inAnchor {
			continue
		}
		if k, _, ok := strings.Cut(ln, "="); ok && strings.TrimSpace(k) == "type" {
			return spliceLine(lines, i, newLine), nil
		}
	}
	return nil, errors.New("frontmatter has no [anchor] table with a type field")
}

func spliceLine(lines []string, i int, replacement string) []string {
	out := make([]string, 0, len(lines))
	out = append(out, lines[:i]...)
	out = append(out, replacement)
	out = append(out, lines[i+1:]...)
	return out
}

func insertAfter(lines []string, i int, newLine string) []string {
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:i+1]...)
	out = append(out, newLine)
	out = append(out, lines[i+1:]...)
	return out
}

// tomlArray renders vals as a TOML array of basic strings, quoting each
// element with entry.go's tomlQuote (shared package-wide; review.go no
// longer keeps its own copy -- see crn-j1uh).
func tomlArray(vals []string) string {
	quoted := make([]string, len(vals))
	for i, v := range vals {
		quoted[i] = tomlQuote(v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// commitCurationPatch writes content to relPath on branch and commits it,
// isolated in a throwaway worktree -- mirroring CommitToReviewBranch's own
// pattern exactly, for the same reason: a checkout-in-place sequence in the
// store's working tree would leave a real corruption window if interrupted
// mid-sequence, which a separate worktree cannot.
func commitCurationPatch(ctx context.Context, store, branch, relPath string, content []byte, topicKey string) error {
	scratch, err := os.MkdirTemp("", "cairn-review-merge-*")
	if err != nil {
		return fmt.Errorf("create curation worktree scratch dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	wt := filepath.Join(scratch, "wt")
	if _, err := gitRun(ctx, store, "worktree", "add", wt, branch); err != nil {
		return fmt.Errorf("check out %q for curation patch: %w", branch, err)
	}
	defer func() { _, _ = gitRun(ctx, store, "worktree", "remove", "--force", wt) }()

	dst := filepath.Join(wt, relPath)
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("prepare curation worktree dir: %w", err)
	}
	// dst stays under wt, a throwaway worktree this function created above;
	// relPath came from git diff --name-only on branch's own commit, not
	// external input.
	//nolint:gosec // dst is confined to a temp worktree, not attacker-controlled
	if err := os.WriteFile(dst, content, 0o600); err != nil {
		return fmt.Errorf("write curated entry: %w", err)
	}
	if _, err := gitRun(ctx, wt, "add", "--", relPath); err != nil {
		return fmt.Errorf("stage curation patch: %w", err)
	}
	if _, err := gitRun(ctx, wt, "commit", "-q", "-m", "librarian: curate "+topicKey); err != nil {
		return fmt.Errorf("commit curation patch: %w", err)
	}
	return nil
}
