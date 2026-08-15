package main

import (
	"os"
	"strings"
	"testing"
)

// TestFreshnessCopyStatesDetectionNotCorrectness pins the defensible framing
// crn-6egs requires: cairn claims evidence-change detection, never that an
// unchanged anchor proves the derived conclusion (or the anchor selection)
// was correct. A reader who believes "fresh" means "still correct" skips the
// re-investigation that is the entire point of the freshness signal.
func TestFreshnessCopyStatesDetectionNotCorrectness(t *testing.T) {
	const want = "Cairn detects changes to declared evidence and prevents affected knowledge from being presented as verified without re-investigation."

	for _, path := range []string{"docs/DESIGN.md", "docs/knowledge-lifecycle.md"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		if !strings.Contains(string(body), want) {
			t.Errorf("%s does not contain the required defensible-promise sentence:\n%s", path, want)
		}
	}
}

// TestDesignDocStatesTypeFieldConvention pins the operator's OKF ruling
// (2026-08-04): adopt OKF's minimum-viable-schema principle -- every entry
// is expected to carry a `type` field, everything else left to the
// producer -- stated as documentation only, no export/interop built.
func TestDesignDocStatesTypeFieldConvention(t *testing.T) {
	body, err := os.ReadFile("docs/DESIGN.md")
	if err != nil {
		t.Fatalf("reading docs/DESIGN.md: %v", err)
	}
	if !strings.Contains(string(body), "`type`") || !strings.Contains(string(body), "OKF") {
		t.Errorf("docs/DESIGN.md does not document the `type`-field minimum-viable-schema convention (OKF-derived)")
	}
}

// TestDesignDocStatesSharedRememberLeavesWorkingTreeUntracked pins the
// crn-q5kk.2 documentation fix: a shared-tier `remember` deliberately leaves
// the store's working-tree copy untracked (`??`) alongside a real commit on
// its own `remember/<id>` branch, so `git status` alone looks like data loss
// to an operator who doesn't also check `git branch -a`/`git log --all`.
// Without this, the gap that produced the original (good-faith but
// incomplete) durability bug report in crn-q5kk stays undocumented.
func TestDesignDocStatesSharedRememberLeavesWorkingTreeUntracked(t *testing.T) {
	body, err := os.ReadFile("docs/DESIGN.md")
	if err != nil {
		t.Fatalf("reading docs/DESIGN.md: %v", err)
	}
	s := string(body)
	for _, want := range []string{
		"leaves the working-tree copy untracked",
		"`git status` alone will not show the commit",
		"git branch -a",
		"remember/*",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("docs/DESIGN.md does not document the untracked-working-tree-copy behavior (missing %q)", want)
		}
	}
}

// TestDesignDocStatesFilesAnchorSourcedFromOriginMain pins the crn-44uuq
// documentation fix: PR #82 changed objectHash (internal/cairn/freshness.go)
// to source a files-anchor fingerprint from origin/main first, falling back
// to HEAD only when origin/main does not resolve -- but both docs still
// described the pre-#82 HEAD-only behavior, contradicting the shipped code
// and its tests (TestFileAnchorFingerprintTracksOriginMainDrift et al.). A
// reader relying on either doc to explain a freshness result would draw the
// wrong conclusion about what "unchanged" actually measures.
func TestDesignDocStatesFilesAnchorSourcedFromOriginMain(t *testing.T) {
	body, err := os.ReadFile("docs/DESIGN.md")
	if err != nil {
		t.Fatalf("reading docs/DESIGN.md: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "origin/main") {
		t.Errorf("docs/DESIGN.md does not name origin/main as the files-anchor fingerprint source")
	}
	if strings.Contains(s, "hashes of those\n  paths at `HEAD`") {
		t.Errorf("docs/DESIGN.md still describes the files-anchor fingerprint as sourced from HEAD unconditionally")
	}

	body, err = os.ReadFile("docs/knowledge-lifecycle.md")
	if err != nil {
		t.Fatalf("reading docs/knowledge-lifecycle.md: %v", err)
	}
	s = string(body)
	if !strings.Contains(s, "origin/main") {
		t.Errorf("docs/knowledge-lifecycle.md does not name origin/main as the files-anchor fingerprint source")
	}
	if strings.Contains(s, "hashes at `HEAD`") {
		t.Errorf("docs/knowledge-lifecycle.md still describes the files-anchor fingerprint as sourced from HEAD unconditionally")
	}
}
