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

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 7 {
		t.Fatalf("got %d lines, want 7:\n%s", len(lines), buf.String())
	}
	wantKinds := []string{"context", "shadow_decision", "freshness_check", "index_drift", "write_path", "write_path_step", "retrieval_outcome"}
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
