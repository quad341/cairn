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
