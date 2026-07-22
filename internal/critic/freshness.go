package critic

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/quad341/cairn/internal/cairn"
)

const freshnessScenarioID = "freshness-anchor-drift"

const freshnessFixtureFile = "a.txt"

// RunFreshnessScenario exercises Check()/ComputeFingerprint()'s 3
// user-visible freshness states — no anchor, never verified, and
// fresh-then-drifted-to-stale — against a real, disposable git repo ("files"
// is the only anchor type computable in v1, per freshness.go) and real
// entries seeded into store via the same Entry.Create path `cairn remember`
// uses, then re-loaded with Find so Check runs against the TOML round-trip,
// not just an in-memory struct.
func RunFreshnessScenario(ctx context.Context, store string) Result {
	n, err := nonce()
	if err != nil {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail, fmt.Sprintf("nonce: %v", err))
	}

	repo, err := os.MkdirTemp("", "cairn-critic-freshness-"+n)
	if err != nil {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail, fmt.Sprintf("create scratch repo: %v", err))
	}
	defer func() { _ = os.RemoveAll(repo) }()

	if err := gitInitAndCommit(ctx, repo, freshnessFixtureFile, "line one\n"); err != nil {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail, fmt.Sprintf("init scratch repo: %v", err))
	}

	if r := checkFreshnessNoAnchor(ctx, store, n); r.Verdict != Pass {
		return r
	}
	if r := checkFreshnessNeverVerified(ctx, store, n, repo); r.Verdict != Pass {
		return r
	}
	return checkFreshnessDriftDetection(ctx, store, n, repo)
}

// checkFreshnessNoAnchor seeds an entry with no anchor at all and asserts
// Check reports Unknown — the "time-based freshness only" case.
func checkFreshnessNoAnchor(ctx context.Context, store, n string) Result {
	e, err := cairn.NewEntry("critic-freshness-no-anchor-"+n, nil, "no-anchor fixture body", "critic")
	if err != nil {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail, fmt.Sprintf("build entry: %v", err))
	}

	cleanup, err := seedEntries(ctx, store, []*cairn.Entry{e})
	defer cleanup()
	if err != nil {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail, fmt.Sprintf("seed fixture: %v", err))
	}

	loaded, err := cairn.Find(store, e.ID)
	if err != nil {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail, fmt.Sprintf("Find: %v", err))
	}
	status, detail := cairn.Check(ctx, loaded)
	if status != cairn.Unknown {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail,
			fmt.Sprintf("no-anchor entry: expected status %q, got %q (%s)", cairn.Unknown, status, detail))
	}
	return NewResult(DimensionFreshness, freshnessScenarioID, Pass, "no-anchor entry correctly reports unknown")
}

// checkFreshnessNeverVerified seeds an entry with a verifiable anchor but no
// stored fingerprint and asserts Check reports Unknown — the
// "never verified" case, distinct from the no-anchor case above.
func checkFreshnessNeverVerified(ctx context.Context, store, n, repo string) Result {
	e, err := cairn.NewEntry("critic-freshness-never-verified-"+n, nil, "never-verified fixture body", "critic")
	if err != nil {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail, fmt.Sprintf("build entry: %v", err))
	}
	e.Anchor = cairn.Anchor{Type: "files", Repo: repo, Paths: []string{freshnessFixtureFile}}

	cleanup, err := seedEntries(ctx, store, []*cairn.Entry{e})
	defer cleanup()
	if err != nil {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail, fmt.Sprintf("seed fixture: %v", err))
	}

	loaded, err := cairn.Find(store, e.ID)
	if err != nil {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail, fmt.Sprintf("Find: %v", err))
	}
	status, detail := cairn.Check(ctx, loaded)
	if status != cairn.Unknown {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail,
			fmt.Sprintf("never-verified entry: expected status %q, got %q (%s)", cairn.Unknown, status, detail))
	}
	return NewResult(DimensionFreshness, freshnessScenarioID, Pass, "never-verified entry correctly reports unknown")
}

// checkFreshnessDriftDetection seeds an entry with a stored fingerprint
// matching the repo's current state (expect Fresh), then mutates and
// re-commits the anchored file (expect Stale) — the drift-detection round
// trip that is the whole point of the freshness dimension.
func checkFreshnessDriftDetection(ctx context.Context, store, n, repo string) Result {
	anchor := cairn.Anchor{Type: "files", Repo: repo, Paths: []string{freshnessFixtureFile}}
	fp := cairn.ComputeFingerprint(ctx, anchor)
	if fp == "" {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail, "ComputeFingerprint returned empty for a valid files anchor")
	}
	anchor.Fingerprint = fp

	e, err := cairn.NewEntry("critic-freshness-drift-"+n, nil, "drift fixture body", "critic")
	if err != nil {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail, fmt.Sprintf("build entry: %v", err))
	}
	e.Anchor = anchor

	cleanup, err := seedEntries(ctx, store, []*cairn.Entry{e})
	defer cleanup()
	if err != nil {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail, fmt.Sprintf("seed fixture: %v", err))
	}

	loaded, err := cairn.Find(store, e.ID)
	if err != nil {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail, fmt.Sprintf("Find: %v", err))
	}
	status, detail := cairn.Check(ctx, loaded)
	if status != cairn.Fresh {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail,
			fmt.Sprintf("pre-drift: expected status %q, got %q (%s)", cairn.Fresh, status, detail))
	}

	if err := commitFile(ctx, repo, freshnessFixtureFile, "line one\nline two\n", "drift"); err != nil {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail, fmt.Sprintf("induce drift: %v", err))
	}

	status, detail = cairn.Check(ctx, loaded)
	if status != cairn.Stale {
		return NewResult(DimensionFreshness, freshnessScenarioID, Fail,
			fmt.Sprintf("post-drift: expected status %q, got %q (%s)", cairn.Stale, status, detail))
	}
	return NewResult(DimensionFreshness, freshnessScenarioID, Pass, "fresh-then-drifted-to-stale transition correctly detected")
}

// gitInitAndCommit creates a git repo at dir and commits file with contents
// as its first commit — the same fixture shape freshness_test.go's
// gitInit/gitCommitAll build, but as runtime code rather than test code,
// since this scenario builds its git fixture at real runtime too.
func gitInitAndCommit(ctx context.Context, dir, file, contents string) error {
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "critic@cairn.local"},
		{"config", "user.name", "cairn-critic"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %s: %w", args, out, err)
		}
	}
	return commitFile(ctx, dir, file, contents, "init")
}

// commitFile writes contents to file inside dir and commits it — used both
// for the initial commit and for the later drift-inducing change.
func commitFile(ctx context.Context, dir, file, contents, msg string) error {
	if err := os.WriteFile(filepath.Join(dir, file), []byte(contents), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", file, err)
	}
	add := exec.CommandContext(ctx, "git", "-C", dir, "add", "-A")
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %s: %w", out, err)
	}
	commit := exec.CommandContext(ctx, "git", "-C", dir, "commit", "-q", "-m", msg)
	if out, err := commit.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %s: %w", out, err)
	}
	return nil
}
