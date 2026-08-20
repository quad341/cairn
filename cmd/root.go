package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/quad341/cairn/internal/obslog"
)

var (
	storeFlag string
	traceFlag bool
)

// commandsWithFreeformPositional lists commands whose positional argument is
// free-text content rather than an identifier -- logging it verbatim in the
// context record would put entry bodies into the debug log (and any rage
// bundle built from it). "remember [body]" is cairn's only such command
// today (cmd/remember.go).
var commandsWithFreeformPositional = map[string]bool{
	"remember": true,
}

// suspiciousFlagNamePattern matches flag names that conventionally carry
// secrets, so their values are redacted regardless of which command they're
// passed to -- unlike commandsWithFreeformPositional, this is a blanket
// safety net rather than a per-command allowlist, because a secret-shaped
// flag can be added to any command later without this check being updated.
var suspiciousFlagNamePattern = regexp.MustCompile(`(?i)token|secret|passwd|password|credential|key`)

// redactArgv returns argv with any values that shouldn't be logged verbatim
// replaced by a placeholder: cmd's free-text positional (see
// commandsWithFreeformPositional) and any flag value whose flag name matches
// suspiciousFlagNamePattern. Only values are redacted -- flag names and
// non-matching argv tokens pass through unchanged. Returns argv unmodified
// (same slice) when neither rule finds anything to redact.
func redactArgv(argv []string, cmd *cobra.Command) []string {
	redact := map[string]bool{}
	if commandsWithFreeformPositional[cmd.Name()] {
		for _, v := range cmd.Flags().Args() {
			redact[v] = true
		}
	}
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if suspiciousFlagNamePattern.MatchString(f.Name) {
			redact[f.Value.String()] = true
		}
	})
	if len(redact) == 0 {
		return argv
	}
	out := make([]string, len(argv))
	for i, a := range argv {
		name, val, hasEq := strings.Cut(a, "=")
		shorthandVal, shorthandOK := redactShorthandBundle(a, hasEq, cmd, redact)
		switch {
		case hasEq && strings.HasPrefix(a, "-") && redact[val]:
			out[i] = name + "=«redacted»"
		case shorthandOK:
			out[i] = shorthandVal
		case redact[a]:
			out[i] = "«redacted»"
		default:
			out[i] = a
		}
	}
	return out
}

// redactShorthandBundle handles the POSIX/GNU shorthand-combined form, where
// one or more no-value (boolean/count-style) shorthands precede a
// value-taking shorthand in the same token with no separator (e.g. -tSECRET,
// or -v -tSECRET bundled as -vtSECRET). It walks the bundle one character at
// a time -- mirroring pflag's own parseSingleShortArg loop -- resolving each
// via cmd's registered shorthands and skipping past any flag whose
// NoOptDefVal is set (it takes no attached value). The first value-taking
// shorthand found (NoOptDefVal == "") consumes the rest of the token as its
// value; only one such flag can appear per bundle, since pflag itself stops
// there. Returns the redacted token and true only when that value is a
// member of redact. Returns ("", false) -- signaling the caller to fall
// through to its other cases -- when the token doesn't have this shape, an
// equals form already claimed it, an unrecognized shorthand character is hit
// (never guess), or the resolved value isn't in redact.
func redactShorthandBundle(a string, hasEq bool, cmd *cobra.Command, redact map[string]bool) (string, bool) {
	if hasEq || strings.HasPrefix(a, "--") || !strings.HasPrefix(a, "-") || len(a) <= 2 {
		return "", false
	}
	for i := 1; i < len(a); i++ {
		f := cmd.Flags().ShorthandLookup(string(a[i]))
		if f == nil {
			return "", false
		}
		if f.NoOptDefVal != "" {
			continue
		}
		val := a[i+1:]
		if !redact[val] {
			return "", false
		}
		return a[:i+1] + "«redacted»", true
	}
	return "", false
}

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
	// SilenceUsage stops Cobra dumping the full usage/help block on every
	// RunE/PersistentPreRunE error. Cobra's ExecuteC checks this field on
	// both the root and the leaf command found; setting it here covers every
	// subcommand without needing the per-command opt-in format.go/
	// remember_batch.go otherwise use. Without it, the real error line sits
	// at the top of a multi-line dump that a `| tail -8` cuts off.
	SilenceUsage: true,
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
			Args:           redactArgv(os.Args, cmd),
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
