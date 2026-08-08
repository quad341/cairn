// Package obslog is cairn's opt-in-visible, always-on debug logger: a
// JSON-lines record of what each invocation decided and why, written to a
// rotating file under the XDG state directory and, with --trace, mirrored
// to stderr.
//
// obslog is deliberately narrow. Its only public write surface is a fixed
// set of typed, per-record-kind methods on *Logger (Context,
// ShadowDecision, FreshnessCheck, IndexDrift, WritePath, WritePathStep) --
// there is no generic Log(msg, attrs...) escape hatch and no method takes
// an entry body or free-form content parameter. Callers in internal/cairn
// pass already-redacted strings for any field that might carry
// user/git-sourced text.
//
// Construction never fails outward: New returns a working no-op logger
// (backed by slog.DiscardHandler) whenever the log path can't be resolved
// or the file can't be opened, so a command's behavior never depends on
// whether logging succeeded.
package obslog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"
)

const (
	maxLogBytes = 10 * 1024 * 1024
	keepRotated = 3
)

// Logger is a handle onto obslog's JSONL sink. The zero value is not
// usable; use FromContext or New. A nil *Logger is never returned by
// either.
type Logger struct {
	sl *slog.Logger
}

type ctxKey struct{}

var noopLogger = &Logger{sl: slog.New(slog.DiscardHandler)}

// WithLogger attaches l to ctx. A nil l attaches the no-op logger, so
// FromContext's never-nil guarantee holds regardless of what callers pass.
func WithLogger(ctx context.Context, l *Logger) context.Context {
	if l == nil {
		l = noopLogger
	}
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext retrieves the Logger attached to ctx by WithLogger. It never
// returns nil: with no attached logger, it returns a no-op logger whose
// methods are safe to call and produce no output.
func FromContext(ctx context.Context) *Logger {
	if l, ok := ctx.Value(ctxKey{}).(*Logger); ok && l != nil {
		return l
	}
	return noopLogger
}

// Options configures a Logger constructed by New.
type Options struct {
	// Command is the resolved cobra command name (e.g. "get", "remember"),
	// attached to every record emitted by the constructed Logger.
	Command string
	// Trace mirrors every record to stderr in addition to the log file.
	Trace bool
}

// New constructs a Logger writing JSONL records to the XDG state log file
// (internal/obslog.LogPath), mirrored to stderr when opts.Trace is set.
// Construction is fail-open: if the log path can't be resolved or the file
// can't be opened, New returns a no-op Logger rather than an error, so the
// calling command proceeds unaffected.
func New(opts Options) *Logger {
	path, err := LogPath()
	if err != nil {
		return noopLogger
	}
	w, err := newRotatingWriter(path, maxLogBytes, keepRotated)
	if err != nil {
		return noopLogger
	}
	return NewWithWriter(w, opts, os.Stderr)
}

// NewWithWriter builds a Logger over an arbitrary sink instead of the real
// XDG log file, and lets a test substitute its own writer for the trace
// mirror instead of the real os.Stderr. New is a thin wrapper over this for
// the production (file + real stderr) case.
func NewWithWriter(file io.Writer, opts Options, trace io.Writer) *Logger {
	out := file
	if opts.Trace {
		out = io.MultiWriter(file, trace)
	}
	h := slog.NewJSONHandler(out, &slog.HandlerOptions{ReplaceAttr: replaceAttr})
	sl := slog.New(h).With(
		slog.String("invocation_id", invocationID()),
		slog.String("command", opts.Command),
	)
	return &Logger{sl: sl}
}

// replaceAttr renames the built-in "time" key to "ts" (matching obslog's
// envelope) and drops "level" and "msg" -- every record already carries a
// "kind" field, so a constant level and an always-empty message are pure
// noise in output meant to be grepped as flat JSON objects.
func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		a.Key = "ts"
	case slog.LevelKey, slog.MessageKey:
		return slog.Attr{}
	}
	return a
}

func invocationID() string {
	return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
}

// ContextFields is the "context" record: logged unconditionally once per
// invocation (FR-1), capturing what the command resolved before doing
// anything else.
type ContextFields struct {
	Version        string
	Commit         string
	StorePath      string
	StoreSource    string // "flag" | "env" | "default"
	IdentityTags   []string
	IdentitySource string // "flag" | "env" | "default"
}

// Context logs a "context" record.
func (l *Logger) Context(f ContextFields) {
	l.sl.Info("", slog.String("kind", "context"),
		slog.String("version", f.Version),
		slog.String("commit", f.Commit),
		slog.String("store_path", f.StorePath),
		slog.String("store_source", f.StoreSource),
		slog.Any("identity_tags", f.IdentityTags),
		slog.String("identity_source", f.IdentitySource),
	)
}

// ShadowDecisionFields is the "shadow_decision" record, logged at both
// shadow-resolution call sites: Mode distinguishes the identity-scoped
// per-get resolution ("identity") from the store-wide sweep ("store_wide").
// EntryID is the entry the decision concerns: empty in "identity" mode
// (shadowReason resolves a whole topic_key group to one winner in a single
// decision), set to the shadowed entry's ID in "store_wide" mode
// (ShadowMap decides shadowing per entry pair).
type ShadowDecisionFields struct {
	Mode     string
	TopicKey string
	EntryID  string
	WinnerID string
	Rule     string
}

// ShadowDecision logs a "shadow_decision" record.
func (l *Logger) ShadowDecision(f ShadowDecisionFields) {
	l.sl.Info("", slog.String("kind", "shadow_decision"),
		slog.String("mode", f.Mode),
		slog.String("topic_key", f.TopicKey),
		slog.String("entry_id", f.EntryID),
		slog.String("winner_id", f.WinnerID),
		slog.String("rule", f.Rule),
	)
}

// FreshnessCheckFields is the "freshness_check" record, logged once per
// entry inside freshness.Check.
type FreshnessCheckFields struct {
	EntryID    string
	AnchorType string
	Status     string
	Detail     string
}

// FreshnessCheck logs a "freshness_check" record.
func (l *Logger) FreshnessCheck(f FreshnessCheckFields) {
	l.sl.Info("", slog.String("kind", "freshness_check"),
		slog.String("entry_id", f.EntryID),
		slog.String("anchor_type", f.AnchorType),
		slog.String("status", f.Status),
		slog.String("detail", f.Detail),
	)
}

// IndexDriftFields is the "index_drift" record, logged once per
// ensureFreshWith call.
type IndexDriftFields struct {
	Stale        bool
	Reindexed    bool
	ReindexCount int
	ReindexError string
	DurationMS   int64
}

// IndexDrift logs an "index_drift" record.
func (l *Logger) IndexDrift(f IndexDriftFields) {
	l.sl.Info("", slog.String("kind", "index_drift"),
		slog.Bool("stale", f.Stale),
		slog.Bool("reindexed", f.Reindexed),
		slog.Int("reindex_count", f.ReindexCount),
		slog.String("reindex_error", f.ReindexError),
		slog.Int64("duration_ms", f.DurationMS),
	)
}

// WritePathFields is the "write_path" record: one per write operation
// (e.g. commit_direct, commit_to_review_branch), logged once at the start
// of the operation.
type WritePathFields struct {
	Operation string
	Scope     []string
	Tier      string
	Private   bool
}

// WritePath logs a "write_path" record.
func (l *Logger) WritePath(f WritePathFields) {
	l.sl.Info("", slog.String("kind", "write_path"),
		slog.String("operation", f.Operation),
		slog.Any("scope", f.Scope),
		slog.String("tier", f.Tier),
		slog.Bool("private", f.Private),
	)
}

// WritePathStepFields is the "write_path_step" record: one per git
// sub-step within a write_path operation (Operation correlates it back to
// the parent record).
type WritePathStepFields struct {
	Operation  string
	Name       string
	Outcome    string
	Detail     string
	DurationMS int64
}

// WritePathStep logs a "write_path_step" record.
func (l *Logger) WritePathStep(f WritePathStepFields) {
	l.sl.Info("", slog.String("kind", "write_path_step"),
		slog.String("operation", f.Operation),
		slog.String("name", f.Name),
		slog.String("outcome", f.Outcome),
		slog.String("detail", f.Detail),
		slog.Int64("duration_ms", f.DurationMS),
	)
}

// RetrievalOutcomeFields is the "retrieval_outcome" record: one per `cairn
// get` invocation, classifying what the lookup actually returned.
// IdentityTags matches ContextFields.IdentityTags's naming so the two kinds
// correlate on the same field. The join against burn-report/transcript
// token usage is (identity, wall-clock time) proximity -- this record's own
// envelope "ts" plus IdentityTags -- never by ID: Claude Code transcripts
// carry no run/task/session ID field (confirmed against burn.py's
// harvest(), which reads only timestamp/model/usage per message). RunID is
// a cairn-internal grouping convenience only (e.g. correlating this record
// with others from the same agent task); it has no matching field on the
// transcript side, so a correlation report must not rely on it for that
// join. Outcome is "hit" (found, not stale), "miss" (no such entry), or
// "stale" (found but freshness-drifted). PayloadTokens is a calibrated
// chars-per-token estimate, not a per-entry measured count -- no offline,
// axis-correct tokenizer is available (evaluated for crn-666s.1: the only
// readily available Go option, tiktoken-go, downloads its BPE tables over
// the network by default, AND is the wrong tokenizer family for a
// Claude-consumed body even if made offline via its embedded-loader
// companion; Anthropic's real count_tokens endpoint needs network+API-key,
// ruled out for a routine per-invocation write path). PayloadTokensMethod
// and PayloadTokensErrorBoundPct carry the estimate's calibration and
// measured spread into the record itself (rather than leaving them only in
// this comment) so a consumer summing payload_tokens cannot mistake it for
// ground truth. The calibration (crn-666s.1): chars-per-token measured
// against real Anthropic-API-reported usage.output_tokens from n=418
// text-only assistant turns (no thinking, no tool_use) across local Claude
// Code transcripts, grouped by message.id -- median 2.64 chars/token,
// p5=2.27/p95=3.09 (~17% envelope). Text-only turns were used because
// tokenization is axis-agnostic (the same BPE vocab applies whether a span
// of text is consumed as input or produced as output, so a clean
// output-side ratio for plain text transfers to estimating an input-side
// body's cost) and because two contaminated alternatives were tried and
// rejected first: (1) pairing one JSONL line's content chars against that
// line's output_tokens without grouping by message.id undercounts chars
// ~5-10x, because Claude Code logs one line per content-block fragment of a
// turn while repeating that turn's cumulative usage on every fragment; (2)
// even grouped by message.id, turns containing thinking or tool_use blocks
// give a chars/token ratio 2-5x lower than text-only turns, because billed
// output_tokens for those block types doesn't correspond 1:1 with the
// stored block's character length (thinking is billed for more than the
// stored/visible thinking text suggests; tool_use's structured generation
// isn't well-approximated by re-serializing its JSON input). Note this
// measures chars-per-token, the same axis-agnostic quantity GAP 2's
// transcript join would need for the reverse direction -- it does NOT
// close GAP 2's ceiling: Claude Code's usage block is still a whole-turn
// aggregate, not attributable to one tool result (e.g. one cairn get's
// output) among others a turn may contain, confirmed against burn.py's
// harvest(), which only sums usage fields already computed by the API per
// message, never per content block. ReuseCount is the entry's
// post-increment hit_count on a hit/stale (Find's own RETURNING value),
// left at zero on a miss.
type RetrievalOutcomeFields struct {
	IdentityTags  []string
	RunID         string
	Outcome       string // "hit" | "miss" | "stale"
	EntryID       string
	PayloadTokens int
	// PayloadTokensMethod names how PayloadTokens was derived, e.g.
	// "calibrated_chars_per_token_v1". Left "" when PayloadTokens wasn't
	// computed (a miss). Always set alongside a nonzero-eligible
	// PayloadTokens so the estimate's provenance travels with the number
	// instead of living only in code comments a downstream consumer never
	// sees.
	PayloadTokensMethod string
	// PayloadTokensErrorBoundPct is the calibration's measured spread as a
	// symmetric-ish percentage envelope around PayloadTokens (see this
	// struct's doc comment for how it was derived). Left 0 when
	// PayloadTokensMethod is "" (a miss) -- never meant to be read as "zero
	// error" on a hit, only as "not applicable."
	PayloadTokensErrorBoundPct int
	ReuseCount                 int
}

// RetrievalOutcome logs a "retrieval_outcome" record.
func (l *Logger) RetrievalOutcome(f RetrievalOutcomeFields) {
	l.sl.Info("", slog.String("kind", "retrieval_outcome"),
		slog.Any("identity_tags", f.IdentityTags),
		slog.String("run_id", f.RunID),
		slog.String("outcome", f.Outcome),
		slog.String("entry_id", f.EntryID),
		slog.Int("payload_tokens", f.PayloadTokens),
		slog.String("payload_tokens_method", f.PayloadTokensMethod),
		slog.Int("payload_tokens_error_bound_pct", f.PayloadTokensErrorBoundPct),
		slog.Int("reuse_count", f.ReuseCount),
	)
}

// PrimeEmitFields is the "prime_emit" record: one per `cairn prime`
// invocation, capturing what was surfaced so a later report can join it
// against RetrievalOutcomeFields -- "what was surfaced at prime time" vs.
// "what was actually looked up later" (crn-894i). IdentityTags and RunID
// match RetrievalOutcomeFields's fields of the same name, including the
// same join caveat: correlation against burn-report/transcript token usage
// is by (identity, wall-clock time) proximity, never by RunID (see
// RetrievalOutcomeFields's doc comment). ItemIDs is the full set of entry
// IDs itemized in the invocation's PrimeResult.Items, in the same order.
// TotalVisible and TruncatedCount mirror PrimeResult's fields of the same
// name.
type PrimeEmitFields struct {
	IdentityTags   []string
	RunID          string
	ItemIDs        []string
	TotalVisible   int
	TruncatedCount int
}

// PrimeEmit logs a "prime_emit" record.
func (l *Logger) PrimeEmit(f PrimeEmitFields) {
	l.sl.Info("", slog.String("kind", "prime_emit"),
		slog.Any("identity_tags", f.IdentityTags),
		slog.String("run_id", f.RunID),
		slog.Any("item_ids", f.ItemIDs),
		slog.Int("total_visible", f.TotalVisible),
		slog.Int("truncated_count", f.TruncatedCount),
	)
}

// ExitFields is the "exit" record: logged exactly once per invocation, at
// the very end, regardless of success or failure. Command is the full
// resolved command path (e.g. "cairn doctor explain"), serialized under the
// JSON key "command_path" rather than "command" -- the envelope already
// writes a "command" key at construction time (Options.Command, the leaf
// command name), and reusing that key here would silently collide under
// encoding/json's unmarshal-into-map, last-key-wins semantics. Flags is the
// names only of flags the invocation explicitly set (never a value) and
// never includes positional arguments -- this log is always-on, never
// opt-in, and command bodies (e.g. cairn remember's entry text) are
// free-form prose that must never reach it.
type ExitFields struct {
	Command  string
	Flags    []string
	ExitCode int
	Error    string
}

// Exit logs an "exit" record.
func (l *Logger) Exit(f ExitFields) {
	l.sl.Info("", slog.String("kind", "exit"),
		slog.String("command_path", f.Command),
		slog.Any("flags", f.Flags),
		slog.Int("exit_code", f.ExitCode),
		slog.String("error", f.Error),
	)
}
