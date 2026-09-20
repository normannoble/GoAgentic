package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
	"github.com/normannoble/GoAgentic/internal/installer"
	"github.com/pmezard/go-difflib/difflib"
)

// ErrCancelled identifies an explicit escape, Ctrl-C, or cancel choice.
var ErrCancelled = errors.New("setup cancelled")

// UI owns all terminal interaction for the installer wizard.
type UI struct {
	ctx     context.Context
	in      io.Reader
	out     io.Writer
	noColor bool
	theme   huh.Theme
}

// New constructs a terminal UI using the supplied streams.
func New(ctx context.Context, in io.Reader, out io.Writer, noColor bool) *UI {
	if ctx == nil {
		ctx = context.Background()
	}
	return &UI{
		ctx:     ctx,
		in:      in,
		out:     out,
		noColor: noColor,
		theme:   Theme(noColor),
	}
}

// ShowIntro explains the transaction boundary before any questions are asked.
func (u *UI) ShowIntro() {
	fmt.Fprintln(u.out)
	fmt.Fprintln(u.out, "Agent Framework Setup")
	fmt.Fprintln(u.out, "Build senior AI collaborators with calibrated autonomy in Claude Code.")
	fmt.Fprintln(u.out, "Nothing is written until you review and approve the complete plan.")
	fmt.Fprintln(u.out)
}

// AskTarget prompts for an existing repository directory and returns its
// canonical absolute path.
func (u *UI) AskTarget(defaultTarget string) (string, error) {
	value := defaultTarget
	field := huh.NewInput().
		Title("Target repository").
		Description("Directory where the framework should be installed").
		Prompt("› ").
		Value(&value).
		Validate(func(candidate string) error {
			_, err := resolveExistingDirectory(candidate)
			return err
		})
	if err := u.run(field); err != nil {
		return "", err
	}
	return resolveExistingDirectory(value)
}

// ConfirmNonGit asks for the explicit safety exception required outside Git.
func (u *UI) ConfirmNonGit(target string) (bool, error) {
	confirmed := false
	field := huh.NewConfirm().
		Title(fmt.Sprintf("%s is not inside a Git repository. Continue anyway?", target)).
		Affirmative("Yes").
		Negative("No").
		Value(&confirmed)
	if err := u.run(field); err != nil {
		return false, err
	}
	return confirmed, nil
}

// AskPrincipal prompts for and validates the human directing the agents.
func (u *UI) AskPrincipal(defaultPrincipal string) (string, error) {
	value := defaultPrincipal
	field := huh.NewInput().
		Title("Principal name").
		Description(`Your name; leave blank to use "the principal"`).
		Prompt("› ").
		Value(&value).
		Validate(func(candidate string) error {
			_, err := installer.ValidatePrincipal(candidate)
			return err
		})
	if err := u.run(field); err != nil {
		return "", err
	}
	return installer.ValidatePrincipal(value)
}

// AskNaming prompts for a naming tradition.
func (u *UI) AskNaming(defaultNaming installer.NamingPool) (installer.NamingPool, error) {
	value := defaultNaming
	choices := make([]huh.Option[installer.NamingPool], 0, 3)
	for _, key := range []installer.NamingPool{
		installer.NamingRoman,
		installer.NamingNorse,
		installer.NamingHellenic,
	} {
		profile := installer.NamingProfiles[key]
		choices = append(choices, huh.NewOption(
			fmt.Sprintf("%s  (%s, …)", profile.Title, firstExamples(profile.Examples, 4)),
			key,
		))
	}
	field := huh.NewSelect[installer.NamingPool]().
		Title("Agent naming tradition").
		Description("Use ↑/↓ and Enter").
		Options(choices...).
		Value(&value)
	if err := u.run(field); err != nil {
		return "", err
	}
	return value, nil
}

// AskComponents prompts for optional installation components.
func (u *UI) AskComponents(
	defaultSkills bool,
	defaultWorkspace bool,
	defaultClaude bool,
	claudeAvailable bool,
	promptSkills bool,
	promptWorkspace bool,
	promptClaude bool,
) (skills bool, workspace bool, claude bool, err error) {
	defaults := componentState{
		skills:    defaultSkills,
		workspace: defaultWorkspace,
		claude:    defaultClaude,
	}
	prompted := componentState{
		skills:    promptSkills,
		workspace: promptWorkspace,
		claude:    promptClaude && claudeAvailable,
	}
	selected := make([]string, 0, 3)
	if prompted.skills && defaults.skills {
		selected = append(selected, "skills")
	}
	if prompted.workspace && defaults.workspace {
		selected = append(selected, "workspace")
	}
	if prompted.claude && defaults.claude {
		selected = append(selected, "claude")
	}

	choices := componentChoices(prompted)

	field := newComponentsField(choices, &selected)
	if runErr := u.run(field); runErr != nil {
		return false, false, false, runErr
	}
	result := mergeComponentSelection(defaults, prompted, selected)
	return result.skills, result.workspace, result.claude, nil
}

func componentChoices(prompted componentState) []huh.Option[string] {
	choices := make([]huh.Option[string], 0, 3)
	if prompted.skills {
		choices = append(choices,
			huh.NewOption("Claude Code skills  /agents:help, start, wrap, close, list, status, next, doctor, harness, schedule, ask, new", "skills"),
		)
	}
	if prompted.workspace {
		choices = append(choices,
			huh.NewOption("Workspace directories  thinking, work, knowledge, outputs", "workspace"),
		)
	}
	if prompted.claude {
		choices = append(choices,
			huh.NewOption("CLAUDE.md integration  managed Agents section", "claude"),
		)
	}
	return choices
}

type componentState struct {
	skills    bool
	workspace bool
	claude    bool
}

func mergeComponentSelection(
	defaults componentState,
	prompted componentState,
	selected []string,
) componentState {
	result := defaults
	if prompted.skills {
		result.skills = contains(selected, "skills")
	}
	if prompted.workspace {
		result.workspace = contains(selected, "workspace")
	}
	if prompted.claude {
		result.claude = contains(selected, "claude")
	}
	return result
}

func newComponentsField(
	choices []huh.Option[string],
	selected *[]string,
) *huh.MultiSelect[string] {
	// Huh counts the title and description inside the field height. Leave one
	// row for each so every component remains visible at once.
	return huh.NewMultiSelect[string]().
		Title("Optional components").
		Description("Use ↑/↓, Space to toggle, Enter to continue").
		Options(choices...).
		Value(selected).
		Height(len(choices) + 2)
}

// ResolveConflicts asks for every unresolved customization decision.
func (u *UI) ResolveConflicts(plan *installer.InstallPlan) error {
	groups := plan.ConflictGroups()
	if len(groups) == 0 {
		return nil
	}

	fmt.Fprintf(u.out, "%d customized file(s) need a decision.\n", len(plan.Conflicts()))
	if len(groups) > 1 {
		strategy, err := u.askSelect(
			"Conflict handling",
			"",
			"review",
			[]huh.Option[string]{
				huh.NewOption("Review each conflict", "review"),
				huh.NewOption("Keep every existing file", "keep"),
				huh.NewOption("Replace every conflicting file", "overwrite"),
				huh.NewOption("Cancel setup", "cancel"),
			},
		)
		if err != nil {
			return err
		}
		switch strategy {
		case "cancel":
			return ErrCancelled
		case "keep", "overwrite":
			for _, group := range groups {
				if err := installer.ResolveConflictGroup(plan, group, strategy == "overwrite"); err != nil {
					return err
				}
			}
			return nil
		}
	}

	for _, group := range groups {
		for {
			changes := plan.GroupedConflicts()[group]
			paths := make([]string, 0, len(changes))
			for _, change := range changes {
				paths = append(paths, change.RelativePath)
			}
			decision, err := u.askSelect(
				fmt.Sprintf("Existing content differs: %s", strings.Join(paths, ", ")),
				"",
				"keep",
				[]huh.Option[string]{
					huh.NewOption("Keep existing", "keep"),
					huh.NewOption("Replace with framework version", "overwrite"),
					huh.NewOption("View diff", "diff"),
					huh.NewOption("Cancel setup", "cancel"),
				},
			)
			if err != nil {
				return err
			}
			switch decision {
			case "diff":
				if err := u.showConflictDiff(changes); err != nil {
					return err
				}
				continue
			case "cancel":
				return ErrCancelled
			default:
				if err := installer.ResolveConflictGroup(plan, group, decision == "overwrite"); err != nil {
					return err
				}
			}
			break
		}
	}
	return nil
}

// ShowPlan renders the complete reviewed plan.
func (u *UI) ShowPlan(plan *installer.InstallPlan) {
	fmt.Fprintln(u.out)
	fmt.Fprintln(u.out, "Installation plan")
	fmt.Fprintln(u.out, planTable(plan, u.noColor, planTableOutputWidth(u.out)))
	for _, note := range plan.Notes {
		fmt.Fprintf(u.out, "Note: %s\n", note)
	}
	counts := plan.Counts()
	fmt.Fprintf(
		u.out,
		"\n%d add, %d update, %d unchanged, %d kept\n",
		counts["add"],
		counts["update"],
		counts["unchanged"],
		counts["keep"],
	)
}

// AskReview asks what to do with a fully resolved, finalized plan.
func (u *UI) AskReview() (string, error) {
	return u.askSelect(
		"Ready to apply this plan?",
		"Nothing has been written yet",
		"apply",
		[]huh.Option[string]{
			huh.NewOption("Apply changes", "apply"),
			huh.NewOption("Change answers", "edit"),
			huh.NewOption("Cancel without writing", "cancel"),
		},
	)
}

// ShowDryRun marks the no-write completion path.
func (u *UI) ShowDryRun() {
	fmt.Fprintln(u.out, "\nDry run complete — nothing was written.")
}

// ShowSuccess reports the completed transaction and the next useful commands.
func (u *UI) ShowSuccess(plan *installer.InstallPlan, result installer.ApplyResult) {
	fmt.Fprintln(u.out)
	if len(result.Written) > 0 {
		fmt.Fprintf(u.out, "Setup complete — wrote %d file(s).\n", len(result.Written))
	} else {
		fmt.Fprintln(u.out, "Already up to date — no files changed.")
	}
	fmt.Fprintf(u.out, "Target: %s\n\n", plan.Options.Target)
	fmt.Fprintln(u.out, "Next steps")
	fmt.Fprintf(u.out, "  1. cd %s\n", plan.Options.Target)
	if plan.Options.InstallSkills {
		fmt.Fprintln(u.out, "  2. Run /agents:new to build your first agent")
		fmt.Fprintln(u.out, "  3. Run /agents:start <name> to activate it")
	} else {
		fmt.Fprintln(u.out, "  2. Install the optional skills when you are ready")
	}
}

func (u *UI) run(field huh.Field) error {
	form := huh.NewForm(huh.NewGroup(field)).
		WithTheme(u.theme).
		WithInput(u.in).
		WithOutput(u.out).
		WithWidth(90)
	err := form.RunWithContext(u.ctx)
	if errors.Is(err, huh.ErrUserAborted) || errors.Is(u.ctx.Err(), context.Canceled) {
		return ErrCancelled
	}
	return err
}

func (u *UI) askSelect(
	title string,
	description string,
	defaultValue string,
	options []huh.Option[string],
) (string, error) {
	value := defaultValue
	field := huh.NewSelect[string]().
		Title(title).
		Description(description).
		Options(options...).
		Value(&value)
	if err := u.run(field); err != nil {
		return "", err
	}
	return value, nil
}

func (u *UI) showConflictDiff(changes []*installer.FileChange) error {
	for _, change := range changes {
		existing := strings.ToValidUTF8(string(change.Existing), "�")
		desired := strings.ToValidUTF8(string(change.Desired), "�")
		diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
			A:        difflib.SplitLines(existing),
			B:        difflib.SplitLines(desired),
			FromFile: "existing/" + filepath.ToSlash(change.RelativePath),
			ToFile:   "framework/" + filepath.ToSlash(change.RelativePath),
			Context:  3,
			Eol:      "\n",
		})
		if err != nil {
			return err
		}
		if diff == "" {
			diff = "No textual difference\n"
		}
		fmt.Fprintf(u.out, "\n%s\n%s", filepath.ToSlash(change.RelativePath), diff)
	}
	return nil
}

func resolveExistingDirectory(value string) (string, error) {
	expanded := strings.TrimSpace(value)
	if expanded == "~" || strings.HasPrefix(expanded, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("Directory does not exist")
		}
		expanded = filepath.Join(home, strings.TrimPrefix(expanded, "~/"))
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", errors.New("Directory does not exist")
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", errors.New("Directory does not exist")
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", errors.New("Directory does not exist")
	}
	if !info.IsDir() {
		return "", errors.New("Target must be a directory")
	}
	return canonical, nil
}

func firstExamples(examples string, count int) string {
	parts := strings.Split(examples, ", ")
	if len(parts) > count {
		parts = parts[:count]
	}
	return strings.Join(parts, ", ")
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func statusLabel(status installer.ChangeStatus) string {
	switch status {
	case installer.ChangeAdd:
		return "Add"
	case installer.ChangeUpdate:
		return "Update"
	case installer.ChangeUnchanged:
		return "No change"
	case installer.ChangeKeep:
		return "Keep"
	case installer.ChangeConflict:
		return "Conflict"
	case installer.ChangeBlocked:
		return "Blocked"
	default:
		return string(status)
	}
}
