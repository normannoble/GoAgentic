package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	agentframework "github.com/normannoble/GoAgentic"
	"github.com/normannoble/GoAgentic/internal/installer"
	frameworkui "github.com/normannoble/GoAgentic/internal/ui"
)

const nonInteractiveError = "Non-interactive installation requires --yes, or use --dry-run to inspect the plan."

type initFlags struct {
	principal string
	naming    string
	conflict  string

	skills      bool
	noSkills    bool
	workspace   bool
	noWorkspace bool
	claude      bool
	noClaude    bool

	allowNonGit bool
	yes         bool
	dryRun      bool
	jsonOutput  bool
	noColor     bool
}

type exitError struct {
	cause      error
	code       int
	jsonOutput bool
	cancelled  bool
}

func (e *exitError) Error() string { return e.cause.Error() }
func (e *exitError) Unwrap() error { return e.cause }

// NewRootCommand constructs the complete CLI without reading global process
// streams, which keeps both the executable and tests on the same code path.
func NewRootCommand(in io.Reader, out io.Writer, errOut io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "goagentic",
		Short:         "Install and update the Agent Framework in a repository.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       agentframework.Version,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("goagentic {{.Version}}\n")
	root.InitDefaultVersionFlag()
	if versionFlag := root.Flags().Lookup("version"); versionFlag != nil {
		versionFlag.Shorthand = ""
		versionFlag.Usage = "Show the installed version."
	}
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &exitError{cause: err, code: 2}
	})
	root.AddCommand(newInitCommand())
	return root
}

// Execute runs the CLI with explicit streams and returns the desired process
// status. It is the only layer that formats errors.
func Execute(
	ctx context.Context,
	args []string,
	in io.Reader,
	out io.Writer,
	errOut io.Writer,
) int {
	root := NewRootCommand(in, out, errOut)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}

	status := &exitError{cause: err, code: 1}
	if !errors.As(err, &status) {
		status = &exitError{cause: err, code: 1}
	}
	if status.code == 0 {
		status.code = 1
	}
	// Cobra validates arguments and parses flags before RunE, so init's bound
	// jsonOutput value is not a reliable signal on those failure paths.
	status.jsonOutput = status.jsonOutput || requestsJSON(args)

	if status.jsonOutput {
		message := formatError(status.cause)
		if status.cancelled {
			message = "cancelled"
		}
		_ = writeCompactJSON(out, struct {
			Error string `json:"error"`
			Code  int    `json:"code"`
		}{Error: message, Code: status.code})
		return status.code
	}

	if status.cancelled {
		fmt.Fprintln(errOut, "\nSetup cancelled. Nothing was written.")
	} else {
		fmt.Fprintf(errOut, "Error: %s\n", formatError(status.cause))
	}
	return status.code
}

func newInitCommand() *cobra.Command {
	flags := &initFlags{conflict: string(installer.ConflictAsk)}
	cmd := &cobra.Command{
		Use:   "init [TARGET]",
		Short: "Install the framework into a repository.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return &exitError{
					cause: fmt.Errorf("accepts at most 1 arg(s), received %d", len(args)),
					code:  2,
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, args, flags)
		},
	}

	cmd.Flags().StringVar(&flags.principal, "principal", "", "Name of the person directing the agents.")
	cmd.Flags().StringVar(&flags.naming, "naming", "", "Agent naming tradition (roman, norse, or hellenic).")
	cmd.Flags().BoolVar(&flags.skills, "skills", false, "Install the /agents:* skills (help, start, list, status, next, doctor, new).")
	cmd.Flags().BoolVar(&flags.noSkills, "no-skills", false, "Do not install the /agents:* skills.")
	cmd.Flags().BoolVar(&flags.workspace, "workspace", false, "Create the standard workspace directories.")
	cmd.Flags().BoolVar(&flags.noWorkspace, "no-workspace", false, "Do not create the standard workspace directories.")
	cmd.Flags().BoolVar(&flags.claude, "claude", false, "Add or update the managed CLAUDE.md agents section.")
	cmd.Flags().BoolVar(&flags.noClaude, "no-claude", false, "Do not update CLAUDE.md.")
	cmd.Flags().StringVar(&flags.conflict, "conflict", string(installer.ConflictAsk), "Conflict policy (ask, keep, overwrite, or fail).")
	cmd.Flags().BoolVar(&flags.allowNonGit, "allow-non-git", false, "Allow installation outside a Git repository.")
	cmd.Flags().BoolVarP(&flags.yes, "yes", "y", false, "Apply without the final confirmation. Does not imply overwrite.")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "Show the plan without writing files.")
	cmd.Flags().BoolVar(&flags.jsonOutput, "json", false, "Emit machine-readable JSON and disable prompts.")
	cmd.Flags().BoolVar(&flags.noColor, "no-color", false, "Disable colored output.")
	return cmd
}

func runInit(cmd *cobra.Command, args []string, flags *initFlags) error {
	noColor := flags.noColor || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb"
	interactive := isTerminal(cmd.InOrStdin()) &&
		isTerminal(cmd.OutOrStdout()) &&
		!flags.yes &&
		!flags.jsonOutput

	coreErr := runInitCore(cmd, args, flags, interactive, noColor)
	if coreErr == nil {
		return nil
	}
	if errors.Is(coreErr, frameworkui.ErrCancelled) || errors.Is(coreErr, context.Canceled) {
		return &exitError{
			cause:      frameworkui.ErrCancelled,
			code:       130,
			jsonOutput: flags.jsonOutput,
			cancelled:  true,
		}
	}
	return &exitError{cause: coreErr, code: 1, jsonOutput: flags.jsonOutput}
}

func runInitCore(
	cmd *cobra.Command,
	args []string,
	flags *initFlags,
	interactive bool,
	noColor bool,
) error {
	if !interactive && !flags.dryRun && !flags.yes {
		return errors.New(nonInteractiveError)
	}
	if err := commandContextError(cmd.Context()); err != nil {
		return err
	}

	toggles, err := resolveToggles(cmd, flags)
	if err != nil {
		return err
	}
	naming, namingSpecified, err := resolveNaming(cmd, flags.naming)
	if err != nil {
		return err
	}
	policy, err := installer.ParseConflictPolicy(strings.ToLower(flags.conflict))
	if err != nil {
		return err
	}

	var targetArgument string
	if len(args) == 1 {
		targetArgument = args[0]
	}
	wizard := frameworkui.New(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), noColor)

	if interactive {
		wizard.ShowIntro()
		var previous *installer.InstallOptions
		for {
			options, err := interactiveOptions(
				wizard,
				targetArgument,
				flags,
				cmd.Flags().Changed("principal"),
				toggles,
				naming,
				namingSpecified,
				previous,
			)
			if err != nil {
				return err
			}
			if err := commandContextError(cmd.Context()); err != nil {
				return err
			}
			plan, err := installer.BuildPlan(options)
			if err != nil {
				return err
			}
			if err := resolvePlan(plan, policy, flags.dryRun, wizard.ResolveConflicts); err != nil {
				return err
			}
			wizard.ShowPlan(plan)

			if flags.dryRun {
				wizard.ShowDryRun()
				return nil
			}
			action, err := wizard.AskReview()
			if err != nil {
				return err
			}
			switch action {
			case "cancel":
				return frameworkui.ErrCancelled
			case "edit":
				copy := options
				previous = &copy
				continue
			}
			if err := commandContextError(cmd.Context()); err != nil {
				return err
			}
			result, err := installer.ApplyPlanContext(cmd.Context(), plan)
			if err != nil {
				return err
			}
			wizard.ShowSuccess(plan, result)
			return nil
		}
	}

	options, err := nonInteractiveOptions(
		targetArgument,
		flags,
		toggles,
		naming,
		namingSpecified,
	)
	if err != nil {
		return err
	}
	plan, err := installer.BuildPlan(options)
	if err != nil {
		return err
	}
	if err := resolvePlan(
		plan,
		policy,
		flags.dryRun,
		func(plan *installer.InstallPlan) error {
			return installer.ApplyConflictPolicy(plan, installer.ConflictAsk)
		},
	); err != nil {
		return err
	}

	if flags.dryRun {
		if flags.jsonOutput {
			return writePrettyJSON(cmd.OutOrStdout(), map[string]any{"plan": plan.Output()})
		}
		wizard.ShowPlan(plan)
		wizard.ShowDryRun()
		return nil
	}

	if err := commandContextError(cmd.Context()); err != nil {
		return err
	}
	result, err := installer.ApplyPlanContext(cmd.Context(), plan)
	if err != nil {
		return err
	}
	if flags.jsonOutput {
		return writePrettyJSON(cmd.OutOrStdout(), map[string]any{
			"plan":   plan.Output(),
			"result": result.Output(),
		})
	}
	wizard.ShowPlan(plan)
	wizard.ShowSuccess(plan, result)
	return nil
}

func resolvePlan(
	plan *installer.InstallPlan,
	policy installer.ConflictPolicy,
	dryRun bool,
	resolveAsk func(*installer.InstallPlan) error,
) error {
	// An ask-mode dry run is an inspection operation. Keep unsafe changes intact
	// so the output can explain why the plan is not ready without forcing a
	// conflict choice or attempting to finalize blocked destinations.
	if dryRun && policy == installer.ConflictAsk && !plan.Ready() {
		plan.Notes = append(
			plan.Notes,
			"The final installation manifest is omitted until conflicts and blocked destinations are resolved.",
		)
		return nil
	}
	if policy == installer.ConflictAsk {
		if err := resolveAsk(plan); err != nil {
			return err
		}
	} else if err := installer.ApplyConflictPolicy(plan, policy); err != nil {
		return err
	}
	return installer.FinalizePlan(plan)
}

type componentToggles struct {
	skills    *bool
	workspace *bool
	claude    *bool
}

func resolveToggles(cmd *cobra.Command, values *initFlags) (componentToggles, error) {
	skills, err := resolveToggle(cmd, "skills", values.skills, "no-skills", values.noSkills)
	if err != nil {
		return componentToggles{}, err
	}
	workspace, err := resolveToggle(
		cmd,
		"workspace",
		values.workspace,
		"no-workspace",
		values.noWorkspace,
	)
	if err != nil {
		return componentToggles{}, err
	}
	claude, err := resolveToggle(cmd, "claude", values.claude, "no-claude", values.noClaude)
	if err != nil {
		return componentToggles{}, err
	}
	return componentToggles{skills: skills, workspace: workspace, claude: claude}, nil
}

func resolveToggle(
	cmd *cobra.Command,
	positiveName string,
	positiveValue bool,
	negativeName string,
	negativeValue bool,
) (*bool, error) {
	positiveSet := cmd.Flags().Changed(positiveName)
	negativeSet := cmd.Flags().Changed(negativeName)
	if positiveSet && negativeSet {
		return nil, fmt.Errorf("--%s and --%s cannot be used together", positiveName, negativeName)
	}
	if positiveSet {
		value := positiveValue
		return &value, nil
	}
	if negativeSet {
		value := !negativeValue
		return &value, nil
	}
	return nil, nil
}

func resolveNaming(
	cmd *cobra.Command,
	value string,
) (installer.NamingPool, bool, error) {
	if !cmd.Flags().Changed("naming") {
		return installer.NamingRoman, false, nil
	}
	naming, err := installer.ParseNamingPool(strings.ToLower(value))
	return naming, true, err
}

func nonInteractiveOptions(
	targetArgument string,
	flags *initFlags,
	toggles componentToggles,
	naming installer.NamingPool,
	namingSpecified bool,
) (installer.InstallOptions, error) {
	targetValue := targetArgument
	if targetValue == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return installer.InstallOptions{}, err
		}
		targetValue = cwd
	}
	target, err := resolveTarget(targetValue)
	if err != nil {
		return installer.InstallOptions{}, err
	}

	principalValue := "the principal"
	if flags.principal != "" {
		principalValue = flags.principal
	}
	principal, err := installer.ValidatePrincipal(principalValue)
	if err != nil {
		return installer.InstallOptions{}, err
	}
	if !namingSpecified {
		naming = installer.NamingRoman
	}
	return installer.InstallOptions{
		Target:          target,
		Principal:       principal,
		Naming:          naming,
		InstallSkills:   valueOr(toggles.skills, true),
		CreateWorkspace: valueOr(toggles.workspace, true),
		IntegrateClaude: valueOr(toggles.claude, isRegularFile(filepath.Join(target, "CLAUDE.md"))),
		AllowNonGit:     flags.allowNonGit,
	}, nil
}

func interactiveOptions(
	wizard *frameworkui.UI,
	targetArgument string,
	flags *initFlags,
	principalSpecified bool,
	toggles componentToggles,
	naming installer.NamingPool,
	namingSpecified bool,
	previous *installer.InstallOptions,
) (installer.InstallOptions, error) {
	defaultTarget := targetArgument
	if previous != nil {
		defaultTarget = previous.Target
	}
	if defaultTarget == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return installer.InstallOptions{}, err
		}
		defaultTarget = cwd
	}

	var target string
	var err error
	if targetArgument != "" {
		target, err = resolveTarget(targetArgument)
	} else {
		target, err = wizard.AskTarget(defaultTarget)
	}
	if err != nil {
		return installer.InstallOptions{}, err
	}

	permittedNonGit := flags.allowNonGit
	if !installer.IsGitRepository(target) && !permittedNonGit {
		permittedNonGit, err = wizard.ConfirmNonGit(target)
		if err != nil {
			return installer.InstallOptions{}, err
		}
		if !permittedNonGit {
			return installer.InstallOptions{}, frameworkui.ErrCancelled
		}
	}

	var principal string
	if principalSpecified {
		principal, err = installer.ValidatePrincipal(flags.principal)
	} else {
		defaultPrincipal := ""
		if previous != nil && previous.Principal != "the principal" {
			defaultPrincipal = previous.Principal
		}
		principal, err = wizard.AskPrincipal(defaultPrincipal)
	}
	if err != nil {
		return installer.InstallOptions{}, err
	}

	selectedNaming := naming
	if !namingSpecified {
		if previous != nil {
			selectedNaming = previous.Naming
		} else {
			selectedNaming = installer.NamingRoman
		}
		selectedNaming, err = wizard.AskNaming(selectedNaming)
		if err != nil {
			return installer.InstallOptions{}, err
		}
	}

	claudeAvailable := isRegularFile(filepath.Join(target, "CLAUDE.md")) || valueOr(toggles.claude, false)
	defaultSkills := defaultComponent(toggles.skills, previous, func(o *installer.InstallOptions) bool {
		return o.InstallSkills
	}, true)
	defaultWorkspace := defaultComponent(toggles.workspace, previous, func(o *installer.InstallOptions) bool {
		return o.CreateWorkspace
	}, true)
	defaultClaude := defaultComponent(toggles.claude, previous, func(o *installer.InstallOptions) bool {
		return o.IntegrateClaude
	}, claudeAvailable)

	selectedSkills := defaultSkills
	selectedWorkspace := defaultWorkspace
	selectedClaude := defaultClaude
	promptSkills, promptWorkspace, promptClaude := promptsForComponents(toggles, claudeAvailable)
	if promptSkills || promptWorkspace || promptClaude {
		selectedSkills, selectedWorkspace, selectedClaude, err = wizard.AskComponents(
			defaultSkills,
			defaultWorkspace,
			defaultClaude,
			claudeAvailable,
			promptSkills,
			promptWorkspace,
			promptClaude,
		)
		if err != nil {
			return installer.InstallOptions{}, err
		}
	}

	return installer.InstallOptions{
		Target:          target,
		Principal:       principal,
		Naming:          selectedNaming,
		InstallSkills:   selectedSkills,
		CreateWorkspace: selectedWorkspace,
		IntegrateClaude: selectedClaude,
		AllowNonGit:     permittedNonGit,
	}, nil
}

func promptsForComponents(
	toggles componentToggles,
	claudeAvailable bool,
) (skills bool, workspace bool, claude bool) {
	return toggles.skills == nil,
		toggles.workspace == nil,
		toggles.claude == nil && claudeAvailable
}

func defaultComponent(
	explicit *bool,
	previous *installer.InstallOptions,
	fromPrevious func(*installer.InstallOptions) bool,
	fallback bool,
) bool {
	if explicit != nil {
		return *explicit
	}
	if previous != nil {
		return fromPrevious(previous)
	}
	return fallback
}

func valueOr(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func resolveTarget(value string) (string, error) {
	expanded := value
	if expanded == "~" || strings.HasPrefix(expanded, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("Target directory does not exist: %s", value)
		}
		expanded = filepath.Join(home, strings.TrimPrefix(expanded, "~/"))
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("Target directory does not exist: %s", value)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("Target directory does not exist: %s", value)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("Target directory does not exist: %s", value)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("Target is not a directory: %s", canonical)
	}
	return canonical, nil
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isTerminal(stream any) bool {
	file, ok := stream.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(int(file.Fd()))
}

func requestsJSON(args []string) bool {
	requested := false
	for _, arg := range args {
		if arg == "--" {
			break
		}
		switch {
		case arg == "--json":
			requested = true
		case strings.HasPrefix(arg, "--json="):
			value, err := strconv.ParseBool(strings.TrimPrefix(arg, "--json="))
			if err == nil {
				requested = value
			}
		}
	}
	return requested
}

func commandContextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return frameworkui.ErrCancelled
	default:
		return nil
	}
}

func writePrettyJSON(writer io.Writer, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return err
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(normalized)
}

func writeCompactJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func formatError(err error) string {
	if errors.Is(err, installer.ErrInstaller) {
		return err.Error()
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		detail := pathError.Err.Error()
		if pathError.Path != "" {
			return fmt.Sprintf("Filesystem error at %s: %s", pathError.Path, detail)
		}
		return "Filesystem error: " + detail
	}
	return err.Error()
}
