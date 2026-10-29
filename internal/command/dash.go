package command

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	printRoot  bool
	view       string
	fleet      bool
	noColor    bool
	width      int
	web        bool
	addr       string
	allowHosts []string
	cfTeam     string
	cfAUD      string
	emails     []string
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
	// --view chose between table and tiles until v0.2.8; kept so old
	// invocations still run.
	cmd.Flags().StringVar(&flags.view, "view", "table", "Ignored: the table is the only layout.")
	_ = cmd.Flags().MarkDeprecated("view", "the table is the only layout")
	cmd.Flags().BoolVar(&flags.fleet, "fleet", false, "Show every workspace under ~/agents (or --root) as one row each; enter opens one.")
	cmd.Flags().BoolVar(&flags.printRoot, "print-root", false, "Print the workspace directory the dashboard would show and exit.")
	cmd.Flags().BoolVar(&flags.noColor, "no-color", false, "Disable color.")
	cmd.Flags().IntVar(&flags.width, "width", 0, "Output width for --once (default: terminal width, else 120).")
	cmd.Flags().BoolVar(&flags.web, "web", false, "Serve the dashboard as a web page; items waiting on you can be answered there (into the agent's inbox). With --fleet, every workspace.")
	cmd.Flags().StringVar(&flags.addr, "addr", "127.0.0.1:7777", "With --web, the address to listen on.")
	cmd.Flags().StringSliceVar(&flags.allowHosts, "allow-host", nil, "With --web, a public hostname requests may use (a Cloudflare Tunnel's); needs --cf-access-team and --cf-access-aud.")
	cmd.Flags().StringVar(&flags.cfTeam, "cf-access-team", "", "With --web, require a Cloudflare Access token from this team domain (<team>.cloudflareaccess.com) on every request.")
	cmd.Flags().StringVar(&flags.cfAUD, "cf-access-aud", "", "With --cf-access-team, the Access application's AUD tag.")
	cmd.Flags().StringSliceVar(&flags.emails, "allow-email", nil, "With --cf-access-team, the only emails let through (on top of the Access policy).")
	return cmd
}

func runDash(cmd *cobra.Command, flags *dashFlags) error {
	if flags.sidebar {
		_, err := dash.ReportSidebar()
		return err
	}
	if flags.web {
		return runWeb(cmd, flags)
	}
	if flags.fleet {
		return runFleet(cmd, flags)
	}
	root, err := dashRoot(flags.root)
	if err != nil {
		return err
	}
	if flags.printRoot {
		fmt.Fprintln(cmd.OutOrStdout(), root)
		return nil
	}
	cache := dash.NewTranscriptCache()
	scan := func(all bool) (*dash.Workspace, error) {
		return dash.Scan(root, dash.Options{
			IncludeRetired: all,
			Herdr:          dash.HerdrPanes,
			T3:             dash.T3Threads,
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

// runFleet shows every workspace under the fleet directory: --root when
// given, else ~/agents.
func runFleet(cmd *cobra.Command, flags *dashFlags) error {
	root := flags.root
	if root == "" {
		root = dash.DefaultFleetRoot()
	}
	cache := dash.NewTranscriptCache()
	opts := func(all bool) dash.Options {
		return dash.Options{IncludeRetired: all, Herdr: dash.HerdrPanes, T3: dash.T3Threads, Transcripts: dash.DefaultTranscripts(), Cache: cache}
	}
	scan := func(all bool) (*dash.Fleet, error) { return dash.ScanFleet(root, opts(all)) }
	wsScan := func(dir string) dash.ScanFunc {
		return func(all bool) (*dash.Workspace, error) { return dash.Scan(dir, opts(all)) }
	}

	out := cmd.OutOrStdout()
	outFile, isFile := out.(*os.File)
	tty := isFile && term.IsTerminal(int(outFile.Fd()))
	noColor := flags.noColor || os.Getenv("NO_COLOR") != "" || !tty
	if flags.once || !tty {
		f, err := scan(flags.all)
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
		dash.WriteFleet(out, f, width, dash.NewStyles(noColor))
		return nil
	}
	return dash.RunFleet(cmd.Context(), scan, wsScan, cmd.InOrStdin(), out, noColor, flags.all)
}

// runWeb serves the dashboard over HTTP: the fleet with --fleet, else the
// one workspace dashRoot finds.
func runWeb(cmd *cobra.Command, flags *dashFlags) error {
	src := dash.WebSource{AllowHosts: flags.allowHosts}
	if (flags.cfTeam == "") != (flags.cfAUD == "") {
		return fmt.Errorf("--cf-access-team and --cf-access-aud go together")
	}
	if len(flags.allowHosts) > 0 && flags.cfTeam == "" {
		return fmt.Errorf("--allow-host needs Cloudflare Access (--cf-access-team, --cf-access-aud): the page can type into your agents' sessions")
	}
	if len(flags.emails) > 0 && flags.cfTeam == "" {
		return fmt.Errorf("--allow-email needs --cf-access-team")
	}
	if flags.cfTeam != "" {
		src.Access = &dash.Access{Team: flags.cfTeam, AUD: flags.cfAUD, Emails: flags.emails}
	}
	if flags.fleet {
		src.FleetRoot = flags.root
		if src.FleetRoot == "" {
			src.FleetRoot = dash.DefaultFleetRoot()
		}
		if len(dash.FleetDirs(src.FleetRoot)) == 0 {
			return fmt.Errorf("no agent workspaces under %s", src.FleetRoot)
		}
	} else {
		root, err := dashRoot(flags.root)
		if err != nil {
			return err
		}
		src.Root = root
	}
	cache := dash.NewTranscriptCache()
	src.Options = func(all bool) dash.Options {
		return dash.Options{IncludeRetired: all, Herdr: dash.HerdrPanes, T3: dash.T3Threads, Transcripts: dash.DefaultTranscripts(), Cache: cache}
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return dash.Serve(ctx, flags.addr, src, cmd.ErrOrStderr())
}

// dashRoot picks the workspace: --root when given; else, under a Herdr
// plugin, the focused pane's directory; else the working directory; else the
// directory most of the Herdr Space's panes share (plugin panes start in the
// plugin's own directory, the focused pane may be the dashboard itself, and a
// taken-over shell may sit anywhere).
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
	if wd, err := os.Getwd(); err == nil {
		if root, err := dash.FindRoot(wd); err == nil {
			return root, nil
		}
	}
	// A plain Herdr pane has no plugin context but knows its Space.
	if herdrWorkspace == "" {
		herdrWorkspace = os.Getenv("HERDR_WORKSPACE_ID")
	}
	if herdrWorkspace != "" {
		if root, err := dash.HerdrWorkspaceRoot(herdrWorkspace); err == nil {
			return root, nil
		}
	}
	return "", dash.ErrNoWorkspace
}
