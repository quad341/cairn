package critic

import (
	"testing"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunRecallScenarioPasses(t *testing.T) {
	store := t.TempDir()
	r := RunRecallScenario(store)
	require.Equal(t, Pass, r.Verdict, "detail: %s", r.Detail)
	assert.Equal(t, DimensionRecall, r.Dimension)
	assert.Equal(t, recallScenarioID, r.ScenarioID)
	assert.NotEmpty(t, r.Timestamp)
}

func TestRunRecallScenarioIsRepeatable(t *testing.T) {
	store := t.TempDir()
	r1 := RunRecallScenario(store)
	require.Equal(t, Pass, r1.Verdict, "detail: %s", r1.Detail)
	r2 := RunRecallScenario(store)
	require.Equal(t, Pass, r2.Verdict, "detail: %s", r2.Detail)
}

func TestRunRecallScenarioCleansUpAfterItself(t *testing.T) {
	store := t.TempDir()
	r := RunRecallScenario(store)
	require.Equal(t, Pass, r.Verdict, "detail: %s", r.Detail)

	entries, err := cairn.IterEntries(store)
	require.NoError(t, err)
	assert.Empty(t, entries, "RunRecallScenario must leave no entries behind in the store after a passing run")
}

func TestDiffRecall(t *testing.T) {
	cases := []struct {
		name        string
		got         map[string]bool
		want        map[string]bool
		notWant     map[string]bool
		wantMissing []string
		wantLeaked  []string
	}{
		{
			name:    "everything expected present, nothing unwanted leaked",
			got:     map[string]bool{"a": true, "c": true},
			want:    map[string]bool{"a": true},
			notWant: map[string]bool{"b": true},
		},
		{
			name:        "expected entry missing is a false negative",
			got:         map[string]bool{},
			want:        map[string]bool{"a": true},
			notWant:     map[string]bool{},
			wantMissing: []string{"a"},
		},
		{
			name:       "unwanted entry present is a false positive",
			got:        map[string]bool{"b": true},
			want:       map[string]bool{},
			notWant:    map[string]bool{"b": true},
			wantLeaked: []string{"b"},
		},
		{
			name:        "missing and leaked at once, both reported sorted",
			got:         map[string]bool{"z-leaked": true, "a-leaked": true},
			want:        map[string]bool{"m-missing": true, "b-missing": true},
			notWant:     map[string]bool{"z-leaked": true, "a-leaked": true},
			wantMissing: []string{"b-missing", "m-missing"},
			wantLeaked:  []string{"a-leaked", "z-leaked"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			missing, leaked := diffRecall(tc.got, tc.want, tc.notWant)
			assert.Equal(t, tc.wantMissing, missing)
			assert.Equal(t, tc.wantLeaked, leaked)
		})
	}
}
