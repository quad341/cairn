package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/quad341/cairn/internal/obslog"
)

var (
	storeFlag string
	traceFlag bool
)

// commandsExemptFromStoreGate lists the commands that never touch the
// knowledge store and so must keep working with no --store/$CAIRN_STORE
// configured: the bare root command (--help/--version) and "version".
var commandsExemptFromStoreGate = map[string]bool{
	"cairn":   true,
	"version": true,
}

// storePathWithSource resolves the knowledge store and reports which source
// it came from ("flag", "env", or "default"): --store flag, then
// $CAIRN_STORE. If neither is set, path is "" -- callers must not treat that
// as the current directory. A store must be explicit; agents that write
// knowledge without one silently corrupt whatever directory they happen to
// be standing in (see rootCmd's PersistentPreRunE, which turns an empty path
// into a hard error before any command with a store dependency runs).
func storePathWithSource() (path, source string) {
	if storeFlag != "" {
		return storeFlag, "flag"
	}
	if s := os.Getenv("CAIRN_STORE"); s != "" {
		return s, "env"
	}
	return "", "default"
}

// storePath resolves the knowledge store: --store flag, then $CAIRN_STORE.
// Returns "" if neither is set -- see storePathWithSource.
func storePath() string {
	path, _ := storePathWithSource()
	return path
}

// longDescription builds rootCmd's --help text, appending the resolved
// debug log path (deliverable: cairn doctor/rage need a well-known path to
// find without guessing, and --help/version are the cheapest place to
// surface it). Omitted if the path can't be resolved -- matching obslog's
// own fail-open philosophy, --help must never error over a logging
// concern.
func longDescription() string {
	desc := "cairn — markers left by the agent who solved it, so the next one\n" +
		"doesn't re-walk the trail. Scoped per rig/role/agent, freshness-anchored."
	if path, err := obslog.LogPath(); err == nil {
		desc += "\n\nDebug log: " + path + " (add --trace to also mirror it to stderr)"
	}
	return desc
}

var rootCmd = &cobra.Command{
	Use:   "cairn",
	Short: "A scoped, freshness-tracked knowledge cache for AI agent fleets",
	Long:  longDescription(),
	// PersistentPreRunE runs for every subcommand invocation (it fires with
	// cmd set to the leaf command actually being executed, not rootCmd
	// itself), wiring an obslog.Logger into the command's context before any
	// RunE body runs, and logging one context record unconditionally so
	// every invocation leaves a trace of what it resolved before doing
	// anything else.
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		logger := obslog.New(obslog.Options{Command: cmd.Name(), Trace: traceFlag})
		cmd.SetContext(obslog.WithLogger(cmd.Context(), logger))

		storeP, storeSource := storePathWithSource()
		identityTags, identitySource := identityWithSource(cmd)
		logger.Context(obslog.ContextFields{
			Version:        version,
			Commit:         commit,
			StorePath:      storeP,
			StoreSource:    storeSource,
			IdentityTags:   identityTags,
			IdentitySource: identitySource,
			Args:           os.Args,
		})

		if storeP == "" && !commandsExemptFromStoreGate[cmd.Name()] {
			return fmt.Errorf("no cairn store configured: pass --store or set $CAIRN_STORE")
		}
		return nil
	},
	// RunE only ever does one of two things: print version (--version/-v)
	// or fall back to help, matching today's bare-`cairn`-prints-help
	// behavior explicitly rather than relying on Cobra's implicit
	// no-RunE-set fallback (which this replaces). rootCmd.Version is
	// deliberately never set: Cobra's own built-in --version flag/template
	// would otherwise fight with the --version flag registered below.
	RunE: func(cmd *cobra.Command, _ []string) error {
		if v, _ := cmd.Flags().GetBool("version"); v {
			return printVersion(cmd)
		}
		return cmd.Help()
	},
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&storeFlag, "store", "",
		"store repo path (required; or set $CAIRN_STORE)")
	rootCmd.PersistentFlags().StringSlice("identity", nil,
		"scope tags for recall, e.g. --identity rig:web,role:reviewer (or $CAIRN_IDENTITY)")
	rootCmd.PersistentFlags().String("run-id", "",
		"correlate this invocation's telemetry with an external run/task id (or $CAIRN_RUN_ID)")
	rootCmd.Flags().BoolP("version", "v", false, "print version information")
	rootCmd.PersistentFlags().BoolVar(&traceFlag, "trace", false,
		"mirror the debug log to stderr in addition to the log file")
}
