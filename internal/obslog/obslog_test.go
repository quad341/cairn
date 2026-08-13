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

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 8 {
		t.Fatalf("got %d lines, want 8:\n%s", len(lines), buf.String())
	}
	wantKinds := []string{"context", "shadow_decision", "freshness_check", "index_drift", "write_path", "write_path_step", "retrieval_outcome", "prime_emit"}
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

// TestContextRecordIncludesArgs covers ContextFields.Args: the invoking
// process's argv must round-trip into the "context" record's "args" field
// so a later rage bundle can answer "what command line produced this run"
// from the log alone.
func TestContextRecordIncludesArgs(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithWriter(&buf, Options{Command: "status"}, &bytes.Buffer{})
	l.Context(ContextFields{Args: []string{"cairn", "status", "--store", "/tmp/x"}})

	line := strings.TrimSpace(buf.String())
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("record line is not valid JSON: %v\nline: %s", err, line)
	}

	rawArgs, ok := rec["args"].([]any)
	if !ok || len(rawArgs) != 4 {
		t.Fatalf("args = %v, want a 4-element array", rec["args"])
	}
	want := []string{"cairn", "status", "--store", "/tmp/x"}
	for i, w := range want {
		if rawArgs[i] != w {
			t.Errorf("args[%d] = %v, want %q", i, rawArgs[i], w)
		}
	}
}
