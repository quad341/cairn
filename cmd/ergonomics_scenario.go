package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/quad341/cairn/internal/critic"
	"github.com/spf13/pflag"
)

const ergonomicsScenarioID = "ergonomics-cli-shape"

// RunErgonomicsScenario exercises 3 CLI ergonomics contracts directly
// against the real cobra command tree that Execute() serves — no shelling
// out to a built binary (the cairn binary itself is gated on crn-di7, which
// is blocked on crn-811) and no mocks: the exact rootCmd.Execute() path
// cmd/commands_test.go itself uses. It lives in package cmd, not
// internal/critic, because it needs rootCmd/rememberCmd's unexported cobra
// state; internal/critic is imported one-directionally for the shared
// Result type so callers get a Result in the same shape as every other
// dimension. Not safe to run concurrently with itself in the same process:
// execRoot swaps the package-level os.Stdout for its duration.
func RunErgonomicsScenario() critic.Result {
	if r := checkStatusRejectsIdentity(); r.Verdict != critic.Pass {
		return r
	}
	if r := checkGetErrorsOnMissingID(); r.Verdict != critic.Pass {
		return r
	}
	return checkMapOutputShape()
}

// checkStatusRejectsIdentity asserts `cairn status --identity ...` errors
// rather than silently ignoring the flag — status is unscoped by design.
func checkStatusRejectsIdentity() critic.Result {
	if err := resetIdentityFlag(); err != nil {
		return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Fail, fmt.Sprintf("reset identity flag: %v", err))
	}
	defer func() { _ = resetIdentityFlag() }()

	store, err := os.MkdirTemp("", "cairn-critic-ergo-status-")
	if err != nil {
		return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Fail, fmt.Sprintf("create scratch store: %v", err))
	}
	defer func() { _ = os.RemoveAll(store) }()

	const want = "does not filter by identity"
	_, runErr := execRoot("status", "--store", store, "--identity", "rig:critic-ergo")
	if runErr == nil {
		return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Fail,
			"status --identity: expected an error, got none")
	}
	if !strings.Contains(runErr.Error(), want) {
		return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Fail,
			fmt.Sprintf("status --identity: expected error to contain %q, got %q", want, runErr.Error()))
	}
	return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Pass, "status correctly rejects an explicit --identity")
}

// checkGetErrorsOnMissingID asserts `cairn get <unknown-id>` errors with a
// message naming the missing id, rather than exiting 0 with empty output.
func checkGetErrorsOnMissingID() critic.Result {
	store, err := os.MkdirTemp("", "cairn-critic-ergo-get-")
	if err != nil {
		return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Fail, fmt.Sprintf("create scratch store: %v", err))
	}
	defer func() { _ = os.RemoveAll(store) }()

	const want = "no entry"
	_, runErr := execRoot("get", "critic-ergo-does-not-exist", "--store", store)
	if runErr == nil {
		return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Fail,
			"get <missing-id>: expected an error, got none")
	}
	if !strings.Contains(runErr.Error(), want) {
		return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Fail,
			fmt.Sprintf("get <missing-id>: expected error to contain %q, got %q", want, runErr.Error()))
	}
	return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Pass, "get correctly errors on a missing id")
}

// checkMapOutputShape seeds exactly one known entry into a scratch store and
// asserts `cairn map`'s output matches the exact expected header+topic-line
// shape byte for byte, rather than merely checking for a nonzero exit.
func checkMapOutputShape() critic.Result {
	if err := resetIdentityFlag(); err != nil {
		return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Fail, fmt.Sprintf("reset identity flag: %v", err))
	}
	defer func() { _ = resetIdentityFlag() }()

	store, err := os.MkdirTemp("", "cairn-critic-ergo-map-")
	if err != nil {
		return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Fail, fmt.Sprintf("create scratch store: %v", err))
	}
	defer func() { _ = os.RemoveAll(store) }()

	const tag = "rig:critic-ergo-map"
	const topic = "critic-ergo-map-topic"
	e, err := cairn.NewEntry(topic, []string{tag}, "ergonomics fixture body", "critic")
	if err != nil {
		return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Fail, fmt.Sprintf("build fixture: %v", err))
	}
	if err := e.Create(store); err != nil {
		return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Fail, fmt.Sprintf("seed fixture: %v", err))
	}

	out, runErr := execRoot("map", "--store", store, "--identity", tag)
	if runErr != nil {
		return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Fail, fmt.Sprintf("map: %v", runErr))
	}

	want := fmt.Sprintf("# cairn map — %d entries visible to identity %v\n  %s  (%d)\n", 1, []string{tag}, topic, 1)
	if out != want {
		return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Fail,
			fmt.Sprintf("map output shape: expected %q, got %q", want, out))
	}
	return critic.NewResult(critic.DimensionErgonomics, ergonomicsScenarioID, critic.Pass,
		"map output matches the exact expected header+topic-line shape")
}

// execRoot runs the cairn CLI in-process with args and returns everything
// written to stdout plus rootCmd.Execute's error. It captures fmt.Println/
// fmt.Printf output via an os.Stdout pipe swap (mirroring
// cmd/commands_test.go's captureStdout) because several RunE functions
// print directly rather than through cmd.OutOrStdout(), so SetOut alone
// would miss their output.
func execRoot(args ...string) (string, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	rootCmd.SetArgs(args)
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	runErr := rootCmd.Execute()

	if err := w.Close(); err != nil {
		return "", err
	}
	out, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(out), runErr
}

// resetIdentityFlag clears the shared rootCmd's --identity flag so
// sequential execRoot calls within one scenario run don't see each other's
// values. rootCmd is a package-level singleton (see cmd/remember_test.go's
// resetRememberFlags for the same concern in tests): StringSlice.Set treats
// a repeat call as an append, not a replace, so pflag.SliceValue.Replace(nil)
// is required, not Set("").
func resetIdentityFlag() error {
	f := rootCmd.PersistentFlags().Lookup("identity")
	if f == nil {
		return errors.New("identity flag not registered on rootCmd")
	}
	sv, ok := f.Value.(pflag.SliceValue)
	if !ok {
		return errors.New("identity flag does not implement pflag.SliceValue")
	}
	if err := sv.Replace(nil); err != nil {
		return err
	}
	f.Changed = false
	return nil
}
