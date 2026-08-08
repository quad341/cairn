package critic

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/quad341/cairn/internal/cairn"
)

// seedEntries creates each entry in store via the real Entry.Create path —
// the same one `cairn remember` uses, never a hand-rolled file — commits
// the result so ensureFresh's git-HEAD staleness check (crn-6az.6.1.2) can
// see the change, and returns a cleanup func that removes exactly the files
// this call created, regardless of how the caller's scenario exits. This is
// what lets a scenario run repeatedly against a real, already-populated,
// possibly concurrently used store: it only ever touches the fixture files
// it made itself. The commit matters even for a single seed-then-query
// call, not just repeats: on a store with no HEAD at all, ensureFresh
// treats an existing index as fresh forever rather than reindexing every
// call, so a store that never becomes git-observable never lets a second,
// independent Visible() call (e.g. a scenario's next check function) see
// this call's fixtures once an earlier one has already built the index.
func seedEntries(ctx context.Context, store string, entries []*cairn.Entry) (func(), error) {
	created := make([]string, 0, len(entries))
	cleanup := func() {
		for _, p := range created {
			_ = os.Remove(p)
		}
	}
	for _, e := range entries {
		if err := e.Create(store); err != nil {
			cleanup()
			return func() {}, err
		}
		created = append(created, e.BodyPath)
	}
	if err := commitFixtures(ctx, store); err != nil {
		cleanup()
		return func() {}, err
	}
	return cleanup, nil
}

// fixtureRepoInit is the git incantation that turns a scratch directory into
// a usable throwaway fixture repo. Shared by every scenario that builds one,
// so the auto-maintenance suppression below can't be fixed in one place and
// forgotten in another.
//
// gc.auto/maintenance.auto are the load-bearing pair. `git commit` ends in
// run_auto_maintenance(), which spawns `git maintenance run --auto --detach`
// — and --detach means it OUTLIVES the commit that started it. On a fixture
// repo of a few hundred objects it goes on to run `git repack` and `git
// pack-objects`, writing .git/objects/pack/.tmp-<pid>-pack while the test
// that created the repo has already returned. t.TempDir()'s RemoveAll then
// races those writes and fails with "unlinkat .../.git/objects: directory
// not empty" — a cleanup-time flake that never touches an assertion, so it
// reads as unrelated infrastructure noise and got refiled four separate
// times (crn-9k30, crn-u8c0, crn-mxvn, crn-tw3bl) before anyone traced it.
// Measured at 13 failures in 30 runs before, 0 in 30 after.
var fixtureRepoInit = [][]string{
	{"init", "-q"},
	{"config", "user.email", "critic@cairn.local"},
	{"config", "user.name", "cairn-critic"},
	{"config", "commit.gpgsign", "false"},
	{"config", "gc.auto", "0"},
	{"config", "maintenance.auto", "false"},
}

// commitFixtures makes store's current on-disk state git-observable.
// Idempotent: it initializes store as a repo (index/ gitignored, since it's
// the rebuildable SQLite index, not source of truth — see IndexPath) the
// first time it's called for a given store, a no-op on later calls.
func commitFixtures(ctx context.Context, store string) error {
	if _, err := os.Stat(filepath.Join(store, ".git")); os.IsNotExist(err) {
		for _, args := range fixtureRepoInit {
			cmd := exec.CommandContext(ctx, "git", append([]string{"-C", store}, args...)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git %v: %s: %w", args, out, err)
			}
		}
		if err := os.WriteFile(filepath.Join(store, ".gitignore"), []byte("index/\n"), 0o600); err != nil {
			return fmt.Errorf("write .gitignore: %w", err)
		}
	}
	add := exec.CommandContext(ctx, "git", "-C", store, "add", "-A")
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %s: %w", out, err)
	}
	commit := exec.CommandContext(ctx, "git", "-C", store, "commit", "-q", "-m", "fixture")
	if out, err := commit.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %s: %w", out, err)
	}
	return nil
}
