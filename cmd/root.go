package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/quad341/cairn/internal/obslog"
)

var (
	storeFlag string
	traceFlag bool
)

// storePathWithSource resolves the knowledge store and reports which source
// it came from ("flag", "env", or "default"): --store flag, then
// $CAIRN_STORE, then cwd.
func storePathWithSource() (path, source string) {
	if storeFlag != "" {
		return storeFlag, "flag"
	}
	if s := os.Getenv("CAIRN_STORE"); s != "" {
		return s, "env"
	}
	return ".", "default"
}

// storePath resolves the knowledge store: --store flag, then $CAIRN_STORE, then cwd.
func storePath() string {
	path, _ := storePathWithSource()
	return path
}

var rootCmd = &cobra.Command{
	Use:   "cairn",
	Short: "A scoped, freshness-tracked knowledge cache for AI agent fleets",
	Long: "cairn — markers left by the agent who solved it, so the next one\n" +
		"doesn't re-walk the trail. Scoped per rig/role/agent, freshness-anchored.",
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
		})
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
		"store repo path (default: $CAIRN_STORE or the current directory)")
	rootCmd.PersistentFlags().StringSlice("identity", nil,
		"scope tags for recall, e.g. --identity rig:web,role:reviewer (or $CAIRN_IDENTITY)")
	rootCmd.Flags().BoolP("version", "v", false, "print version information")
	rootCmd.PersistentFlags().BoolVar(&traceFlag, "trace", false,
		"mirror the debug log to stderr in addition to the log file")
}
