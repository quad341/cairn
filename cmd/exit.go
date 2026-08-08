package cmd

import (
	"github.com/quad341/cairn/internal/obslog"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// logCommandExit logs the "exit" record every invocation produces exactly
// once, at the very end, regardless of success or failure (FR-8/FR-9). It's
// the single call site root.go's executeAndExit (the common path) and
// doctor.go's two early os.Exit sites (which bypass ExecuteC's normal
// return) all share, so every exit path produces the same record shape.
// Only explicitly-set flag names are logged, via Flags().Visit -- never a
// flag's value, and never a positional argument (ExitFields has no field for
// either), since this log is always-on, never opt-in, and a command body
// (e.g. cairn remember's entry text) is free-form prose.
func logCommandExit(cmd *cobra.Command, code int, err error) {
	var flags []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		flags = append(flags, f.Name)
	})

	msg := ""
	if err != nil {
		msg = err.Error()
	}

	obslog.FromContext(cmd.Context()).Exit(obslog.ExitFields{
		Command:  cmd.CommandPath(),
		Flags:    flags,
		ExitCode: code,
		Error:    msg,
	})
}
