package critic

import (
	"os"

	"github.com/quad341/cairn/internal/cairn"
)

// seedEntries creates each entry in store via the real Entry.Create path —
// the same one `cairn remember` uses, never a hand-rolled file — and
// returns a cleanup func that removes exactly the files this call created,
// regardless of how the caller's scenario exits. This is what lets a
// scenario run repeatedly against a real, already-populated, possibly
// concurrently used store: it only ever touches the fixture files it made
// itself.
func seedEntries(store string, entries []*cairn.Entry) (func(), error) {
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
	return cleanup, nil
}
