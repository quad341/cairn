package cmd

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/quad341/cairn/internal/cairn"
	"github.com/spf13/cobra"
)

// ErrorCategory is a stable, machine-readable classification for cairn
// --json's error envelope. These four values are part of the CLI's public
// contract: once shipped, a category must never be renamed or repurposed.
type ErrorCategory string

// The five stable ErrorCategory values in cairn --json's error envelope —
// see ErrorCategory's doc for the stability contract.
const (
	CategoryNotFound       ErrorCategory = "not_found"
	CategoryInvalidInput   ErrorCategory = "invalid_input"
	CategoryMalformedStore ErrorCategory = "malformed_store"
	CategoryInternal       ErrorCategory = "internal"
	// CategoryConflict marks a request that was well-formed but discarded
	// because it collided with existing state -- e.g. cairn remember
	// (crn-qxj3) declining to store a near-identical recurrence of an
	// existing entry.
	CategoryConflict ErrorCategory = "conflict"
)

// ErrorDetail is the body of cairn --json's error envelope.
type ErrorDetail struct {
	Category ErrorCategory `json:"category"`
	Message  string        `json:"message"`
	Subject  string        `json:"subject,omitempty"`
}

// ErrorResult is cairn --json's top-level error shape.
type ErrorResult struct {
	Error ErrorDetail `json:"error"`
}

// classifiedError attaches a stable category and subject to an error while
// preserving both its exact rendered text and its errors.Is/errors.As chain:
// Error() returns the wrapped error's text unchanged, Unwrap() returns the
// wrapped error itself.
type classifiedError struct {
	category ErrorCategory
	subject  string
	err      error
}

func (c *classifiedError) Error() string { return c.err.Error() }
func (c *classifiedError) Unwrap() error { return c.err }

// classifiedErr wraps err with a stable category and subject for cairn
// --json's error envelope. subject is the id/path/etc. the error is about,
// or "" if there is none.
func classifiedErr(category ErrorCategory, subject string, err error) error {
	return &classifiedError{category: category, subject: subject, err: err}
}

// classify turns any error into cairn --json's stable error envelope. A
// classifiedError is read directly; otherwise classify falls back to
// inspecting the error chain for cairn's own sentinel/typed errors, and
// defaults to CategoryInternal.
func classify(err error) ErrorResult {
	var ce *classifiedError
	if errors.As(err, &ce) {
		return ErrorResult{Error: ErrorDetail{Category: ce.category, Message: ce.Error(), Subject: ce.subject}}
	}

	var malformed *cairn.MalformedEntryError
	switch {
	case errors.Is(err, cairn.ErrNotFound):
		return ErrorResult{Error: ErrorDetail{Category: CategoryNotFound, Message: err.Error()}}
	case errors.As(err, &malformed):
		return ErrorResult{Error: ErrorDetail{Category: CategoryMalformedStore, Message: err.Error(), Subject: malformed.Path}}
	default:
		return ErrorResult{Error: ErrorDetail{Category: CategoryInternal, Message: err.Error()}}
	}
}

// wantsJSON reports whether --json was passed.
func wantsJSON(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("json")
	return v
}

// emitJSON writes v to w as indented JSON, matching the encoding convention
// already established by cairn sweep/dedup's --json output.
func emitJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// emitError renders err as cairn --json's error envelope when --json was
// requested, and silences cobra's default usage/error printing so the JSON
// envelope is the only thing written. It always returns err unchanged, so
// the caller's normal (non-JSON) error path -- and root's os.Exit(1) -- is
// unaffected either way.
func emitError(cmd *cobra.Command, err error) error {
	if !wantsJSON(cmd) || err == nil {
		return err
	}
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if jsonErr := emitJSON(cmd.OutOrStdout(), classify(err)); jsonErr != nil {
		return jsonErr
	}
	return err
}

// nonNil normalizes a nil slice to an empty one so --json output always
// serializes the field as [] rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func init() {
	rootCmd.PersistentFlags().Bool("json", false, "emit machine-readable JSON output")
}
