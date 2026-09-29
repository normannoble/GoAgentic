package command

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/normannoble/GoAgentic/internal/dash"
)

type dashFlags struct {
	root       string
	all        bool
	once       bool
	detail     bool
	jsonOutput bool
	summary    bool
	sidebar    bool
	noColor    bool
	width      int
}

func newDashCommand() *cobra.Command {
	flags := &dashFlags{}
	cmd := &cobra.Command{
		Use:   "dash",
		Short: "Show a live, read-only status dashboard for one agent workspace.",
		Long: `Show what every agent in a workspace wants to do next, what it did last,
when it last ran, and whether its files are healthy — read straight from the
agent files, with no coding-agent session running.

The workspace is the nearest directory at or above the current one that holds
agents/CONVENTIONS.md, or the one given with --root.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDash(cmd, flags)
		},
	}
	cmd.Flags().StringVar(&flags.root, "root", "", "Workspace directory (default: search upward from the current directory).")
	cmd.Flags().BoolVar(&flags.all, "all", false, "Include retired agents.")
	cmd.Flags().BoolVar(&flags.once, "once", false, "Print one snapshot and exit.")
	cmd.Flags().BoolVar(&flags.detail, "detail", false, "With --once, add each agent's full detail.")
	cmd.Flags().BoolVar(&flags.jsonOutput, "json", false, "Print one snapshot as JSON and exit.")
	cmd.Flags().BoolVar(&flags.summary, "summary", false, "Print the one-line workspace summary and exit.")
	cmd.Flags().BoolVar(&flags.sidebar, "herdr-sidebar", false, "Write every Herdr workspace's summary to its $agents sidebar token and exit.")
	cmd.Flags().BoolVar(&flags.noColor, "no-color", false, "Disable color.")
	cmd.Flags().IntVar(&flags.width, "width", 0, "Output width for --once (default: terminal width, else 120).")
	return cmd
}

func runDash(cmd *cobra.Command, flags *dashFlags) error {
	if flags.sidebar {
		_, err := dash.ReportSidebar()
		return err
	}
	root, err := dashRoot(flags.root)
	if err != nil {
		return err
	}
	cache := dash.NewTranscriptCache()
	scan := func(all bool) (*dash.Workspace, error) {
		return dash.Scan(root, dash.Options{
			IncludeRetired: all,
			Herdr:          dash.HerdrPanes,
			Transcripts:    dash.DefaultTranscripts(),
			Cache:          cache,
		})
	}

	out := cmd.OutOrStdout()
	outFile, isFile := out.(*os.File)
	tty := isFile && term.IsTerminal(int(outFile.Fd()))
	noColor := flags.noColor || os.Getenv("NO_COLOR") != "" || !tty

	switch {
	case flags.jsonOutput:
		ws, err := scan(flags.all)
		if err != nil {
			return err
		}
		return dash.WriteJSON(out, ws)
	case flags.summary:
		ws, err := scan(flags.all)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, ws.Summary())
		return nil
	case flags.once || !tty:
		ws, err := scan(flags.all)
		if err != nil {
			return err
		}
		width := flags.width
		if width <= 0 {
			width = 120
			if tty {
				if w, _, err := term.GetSize(int(outFile.Fd())); err == nil && w > 0 {
					width = w
				}
			}
		}
		dash.WriteSnapshot(out, ws, width, flags.detail, dash.NewStyles(noColor))
		return nil
	}
	return dash.Run(cmd.Context(), scan, cmd.InOrStdin(), out, noColor, flags.all)
}

// dashRoot picks the workspace: --root when given; else, under a Herdr
// plugin, the focused pane's directory, then the directory most of the Herdr
// workspace's panes share (plugin panes start in the plugin's own directory,
// and the focused pane may be the dashboard itself); else the working
// directory.
func dashRoot(flagRoot string) (string, error) {
	if flagRoot != "" {
		return dash.FindRoot(flagRoot)
	}
	var candidates []string
	herdrWorkspace := ""
	if raw := os.Getenv("HERDR_PLUGIN_CONTEXT_JSON"); raw != "" {
		var ctx struct {
			WorkspaceID    string `json:"workspace_id"`
			FocusedPaneCWD string `json:"focused_pane_cwd"`
			WorkspaceCWD   string `json:"workspace_cwd"`
		}
		if json.Unmarshal([]byte(raw), &ctx) == nil {
			candidates = append(candidates, ctx.FocusedPaneCWD, ctx.WorkspaceCWD)
			herdrWorkspace = ctx.WorkspaceID
		}
	}
	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		if root, err := dash.FindRoot(dir); err == nil {
			return root, nil
		}
	}
	if herdrWorkspace != "" {
		if root, err := dash.HerdrWorkspaceRoot(herdrWorkspace); err == nil {
			return root, nil
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return dash.FindRoot(wd)
}
