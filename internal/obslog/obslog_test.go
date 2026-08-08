package obslog

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestFromContextReturnsNoopWhenAbsent(t *testing.T) {
	l := FromContext(context.Background())
	if l == nil {
		t.Fatal("FromContext returned nil, must never return nil")
	}
	// Must not panic and must not write anywhere observable.
	l.FreshnessCheck(FreshnessCheckFields{EntryID: "e1", Status: "Fresh"})
}

func TestWithLoggerRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf, Options{Command: "get"}, &bytes.Buffer{})
	ctx := WithLogger(context.Background(), l)
	got := FromContext(ctx)
	if got != l {
		t.Error("FromContext(WithLogger(ctx, l)) did not return the same logger instance")
	}
}

func TestWithLoggerNilFallsBackToNoop(t *testing.T) {
	ctx := WithLogger(context.Background(), nil)
	got := FromContext(ctx)
	if got == nil {
		t.Fatal("FromContext returned nil after WithLogger(ctx, nil)")
	}
	got.FreshnessCheck(FreshnessCheckFields{EntryID: "e1", Status: "Fresh"}) // must not panic
}

func TestRecordEnvelopeFlattenedNoLevelOrMsg(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf, Options{Command: "get"}, &bytes.Buffer{})
	l.FreshnessCheck(FreshnessCheckFields{EntryID: "e1", AnchorType: "git_commit", Status: "Fresh", Detail: "ok"})

	line := strings.TrimSpace(buf.String())
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("record line is not valid JSON: %v\nline: %s", err, line)
	}
	for _, forbidden := range []string{"level", "msg"} {
		if _, ok := rec[forbidden]; ok {
			t.Errorf("record contains %q, want it stripped: %v", forbidden, rec)
		}
	}
	for _, want := range []string{"ts", "kind", "invocation_id", "command", "entry_id", "anchor_type", "status", "detail"} {
		if _, ok := rec[want]; !ok {
			t.Errorf("record missing %q: %v", want, rec)
		}
	}
	if rec["kind"] != "freshness_check" {
		t.Errorf("kind = %v, want freshness_check", rec["kind"])
	}
	if rec["command"] != "get" {
		t.Errorf("command = %v, want %q", rec["command"], "get")
	}
}

func TestInvocationIDStableAcrossRecordsFromOneLogger(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf, Options{Command: "get"}, &bytes.Buffer{})
	l.FreshnessCheck(FreshnessCheckFields{EntryID: "e1", Status: "Fresh"})
	l.FreshnessCheck(FreshnessCheckFields{EntryID: "e2", Status: "Stale"})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), buf.String())
	}
	var first, second map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 1: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("line 2: %v", err)
	}
	if first["invocation_id"] != second["invocation_id"] {
		t.Errorf("invocation_id changed across records from the same logger: %v vs %v", first["invocation_id"], second["invocation_id"])
	}
	if first["invocation_id"] == "" || first["invocation_id"] == nil {
		t.Error("invocation_id is empty")
	}
}

func TestTraceMirrorsToSecondWriter(t *testing.T) {
	var file, stderr bytes.Buffer
	l := NewWithWriter(&file, Options{Command: "get", Trace: true}, &stderr)
	l.FreshnessCheck(FreshnessCheckFields{EntryID: "e1", Status: "Fresh"})

	if file.Len() == 0 {
		t.Error("file writer got no output with Trace: true")
	}
	if stderr.Len() == 0 {
		t.Error("stderr writer got no output with Trace: true")
	}
	if file.String() != stderr.String() {
		t.Errorf("file and stderr diverged:\nfile:   %q\nstderr: %q", file.String(), stderr.String())
	}
}

func TestNoTraceOnlyWritesFile(t *testing.T) {
	var file, stderr bytes.Buffer
	l := NewWithWriter(&file, Options{Command: "get", Trace: false}, &stderr)
	l.FreshnessCheck(FreshnessCheckFields{EntryID: "e1", Status: "Fresh"})

	if file.Len() == 0 {
		t.Error("file writer got no output")
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr writer got output with Trace: false: %q", stderr.String())
	}
}

func TestNewFailsOpenWhenPathUnresolvable(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	l := New(Options{Command: "get"})
	if l == nil {
		t.Fatal("New returned nil, must fail open to a no-op logger")
	}
	l.FreshnessCheck(FreshnessCheckFields{EntryID: "e1", Status: "Fresh"}) // must not panic
}

func TestAllRecordKindsProduceValidJSON(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf, Options{Command: "status"}, &bytes.Buffer{})

	l.Context(ContextFields{Version: "v1", Commit: "abc", StorePath: ".", StoreSource: "default", IdentityTags: []string{"rig:web"}, IdentitySource: "flag"})
	l.ShadowDecision(ShadowDecisionFields{Mode: "identity", TopicKey: "t1", WinnerID: "e1", Rule: "specificity"})
	l.FreshnessCheck(FreshnessCheckFields{EntryID: "e1", AnchorType: "git_commit", Status: "Fresh", Detail: "d"})
	l.IndexDrift(IndexDriftFields{Stale: true, Reindexed: true, ReindexCount: 3, DurationMS: 12})
	l.WritePath(WritePathFields{Operation: "commit_direct", Scope: nil, Tier: "private", Private: true})
	l.WritePathStep(WritePathStepFields{Operation: "commit_direct", Name: "git_add", Outcome: "ok", DurationMS: 5})
	l.RetrievalOutcome(RetrievalOutcomeFields{IdentityTags: []string{"rig:web"}, RunID: "run-1", Outcome: "hit", EntryID: "e1", PayloadTokens: 42, ReuseCount: 3})
	l.PrimeEmit(PrimeEmitFields{IdentityTags: []string{"rig:web"}, RunID: "run-1", ItemIDs: []string{"e1"}, TotalVisible: 1, TruncatedCount: 0})
	l.Exit(ExitFields{Command: "status", Flags: []string{"store"}, ExitCode: 0, Error: ""})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 9 {
		t.Fatalf("got %d lines, want 9:\n%s", len(lines), buf.String())
	}
	wantKinds := []string{"context", "shadow_decision", "freshness_check", "index_drift", "write_path", "write_path_step", "retrieval_outcome", "prime_emit", "exit"}
	for i, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d not valid JSON: %v\nline: %s", i, err, line)
		}
		if rec["kind"] != wantKinds[i] {
			t.Errorf("line %d kind = %v, want %v", i, rec["kind"], wantKinds[i])
		}
	}
}

// TestPrimeEmitRecordShape covers crn-jkth's core acceptance criterion: a
// "prime_emit" record must carry identity_tags/run_id (the same
// burn-report/transcript join fields RetrievalOutcomeFields uses, see its
// own doc comment) plus item_ids/total_visible/truncated_count sourced from
// a PrimeResult, so a later report can join "what was surfaced at prime
// time" against retrieval_outcome's "what was actually looked up later"
// (crn-894i).
func TestPrimeEmitRecordShape(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf, Options{Command: "prime"}, &bytes.Buffer{})
	l.PrimeEmit(PrimeEmitFields{
		IdentityTags:   []string{"rig:web"},
		RunID:          "run-1",
		ItemIDs:        []string{"g/a", "g/b"},
		TotalVisible:   5,
		TruncatedCount: 3,
	})

	line := strings.TrimSpace(buf.String())
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("record line is not valid JSON: %v\nline: %s", err, line)
	}

	if rec["kind"] != "prime_emit" {
		t.Errorf("kind = %v, want prime_emit", rec["kind"])
	}
	if rec["run_id"] != "run-1" {
		t.Errorf("run_id = %v, want run-1", rec["run_id"])
	}
	if rec["total_visible"] != float64(5) {
		t.Errorf("total_visible = %v, want 5", rec["total_visible"])
	}
	if rec["truncated_count"] != float64(3) {
		t.Errorf("truncated_count = %v, want 3", rec["truncated_count"])
	}

	tags, ok := rec["identity_tags"].([]any)
	if !ok || len(tags) != 1 || tags[0] != "rig:web" {
		t.Errorf("identity_tags = %v, want [rig:web]", rec["identity_tags"])
	}
	ids, ok := rec["item_ids"].([]any)
	if !ok || len(ids) != 2 || ids[0] != "g/a" || ids[1] != "g/b" {
		t.Errorf("item_ids = %v, want [g/a g/b]", rec["item_ids"])
	}
}

// TestExitRecordShape covers crn-n5yaz's FR-8/FR-9 core acceptance
// criterion: every invocation logs exactly one "exit" record carrying the
// resolved command path, the names of flags explicitly set, the process
// exit code, and any top-level error text. Command is asserted under the
// "command_path" key, not "command" -- the envelope already writes a
// "command" key at construction time (Options.Command, the leaf command
// name); reusing that key for ExitFields.Command would silently collide
// under encoding/json's unmarshal-into-map, last-key-wins semantics.
func TestExitRecordShape(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf, Options{Command: "doctor"}, &bytes.Buffer{})
	l.Exit(ExitFields{
		Command:  "cairn doctor explain",
		Flags:    []string{"store", "json"},
		ExitCode: 1,
		Error:    "2 findings",
	})

	line := strings.TrimSpace(buf.String())
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("record line is not valid JSON: %v\nline: %s", err, line)
	}

	if rec["kind"] != "exit" {
		t.Errorf("kind = %v, want exit", rec["kind"])
	}
	if rec["command_path"] != "cairn doctor explain" {
		t.Errorf("command_path = %v, want %q", rec["command_path"], "cairn doctor explain")
	}
	if rec["exit_code"] != float64(1) {
		t.Errorf("exit_code = %v, want 1", rec["exit_code"])
	}
	if rec["error"] != "2 findings" {
		t.Errorf("error = %v, want %q", rec["error"], "2 findings")
	}
	flags, ok := rec["flags"].([]any)
	if !ok || len(flags) != 2 || flags[0] != "store" || flags[1] != "json" {
		t.Errorf("flags = %v, want [store json]", rec["flags"])
	}
	// command_path must never collide with the envelope's own "command" key
	// (the leaf command name set at NewWithWriter time).
	if rec["command"] != "doctor" {
		t.Errorf("command = %v, want unchanged envelope value %q (must not be overwritten by ExitFields.Command)", rec["command"], "doctor")
	}
}

// TestExitRecordOmitsPositionalArgsAndFlagValues covers the design's
// redaction guardrail (OQ3): the "exit" record's Flags field carries only
// flag *names* -- Exit itself has no way to smuggle a flag value or
// positional argument into the record, because ExitFields has no field for
// either. This test pins that shape: adding an Args []string field or a
// map[string]string of flag values back onto ExitFields would be a
// regression, since cairn remember's entry-body argument is free-form prose
// and this log is always-on, never opt-in.
func TestExitRecordOmitsPositionalArgsAndFlagValues(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf, Options{Command: "remember"}, &bytes.Buffer{})
	l.Exit(ExitFields{
		Command:  "cairn remember",
		Flags:    []string{"store"},
		ExitCode: 0,
	})

	line := strings.TrimSpace(buf.String())
	if strings.Contains(line, "MARKER-TEXT") {
		t.Errorf("exit record must never contain positional argument content, got: %s", line)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("record line is not valid JSON: %v\nline: %s", err, line)
	}
	for _, forbidden := range []string{"args", "positional", "flag_values"} {
		if _, ok := rec[forbidden]; ok {
			t.Errorf("exit record contains forbidden key %q: %v", forbidden, rec)
		}
	}
}
