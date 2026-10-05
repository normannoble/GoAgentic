package command

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	agentframework "github.com/normannoble/GoAgentic"
	"github.com/normannoble/GoAgentic/internal/herdrsetup"
)

func newHerdrCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "herdr",
		Short: "Install the agent dashboard into Herdr on this machine.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	var key, fleetKey string
	var noKey bool
	install := &cobra.Command{
		Use:   "install",
		Short: "Install or update the goagentic.dash Herdr plugin and its keybinding.",
		Long: `Write the dashboard plugin (embedded in this binary) to
~/.local/share/goagentic/herdr-plugin, register it with Herdr, and bind a key
that opens the dashboard. Run it again after upgrading goagentic to update the
plugin; an existing keybinding is left alone.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts, err := herdrOptions(cmd)
			if err != nil {
				return err
			}
			if !noKey {
				opts.Key, opts.FleetKey = key, fleetKey
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Installing the GoAgentic dashboard into Herdr:")
			if err := herdrsetup.Install(opts); err != nil {
				return err
			}
			if noKey {
				fmt.Fprintln(cmd.OutOrStdout(), "\nOpen it with: herdr plugin action invoke goagentic.dash.board")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "\nPress %s in any Space to open the dashboard", key)
				if fleetKey != "" {
					fmt.Fprintf(cmd.OutOrStdout(), ", %s for every workspace", fleetKey)
				}
				fmt.Fprintln(cmd.OutOrStdout(), ".")
			}
			return nil
		},
	}
	install.Flags().StringVar(&key, "key", herdrsetup.DefaultKey, "Key that opens the dashboard.")
	install.Flags().StringVar(&fleetKey, "fleet-key", herdrsetup.DefaultFleetKey, "Key that opens the fleet view (every workspace); empty skips it.")
	install.Flags().BoolVar(&noKey, "no-key", false, "Do not add keybindings.")

	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the plugin, its keybinding, and its files.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts, err := herdrOptions(cmd)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Removing the GoAgentic dashboard from Herdr:")
			return herdrsetup.Uninstall(opts)
		},
	}

	status := &cobra.Command{
		Use:   "status",
		Short: "Show whether the plugin and keybinding are installed.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts, err := herdrOptions(cmd)
			if err != nil {
				return err
			}
			s, err := herdrsetup.Status(opts)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), s)
			return nil
		},
	}

	var index int
	moveTab := &cobra.Command{
		Use:    "move-tab <tab-id>",
		Short:  "Move a Herdr tab to a position in its Space (0 is leftmost). Used by the plugin.",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return herdrsetup.MoveTab(os.Getenv("HERDR_SOCKET_PATH"), args[0], index)
		},
	}
	moveTab.Flags().IntVar(&index, "index", 0, "Position in the tab row, 0 = leftmost.")

	cmd.AddCommand(install, uninstall, status, moveTab)
	return cmd
}

func herdrOptions(cmd *cobra.Command) (herdrsetup.Options, error) {
	files, err := fs.Sub(agentframework.HerdrPlugin, "integrations/herdr")
	if err != nil {
		return herdrsetup.Options{}, err
	}
	binary, err := os.Executable()
	if err != nil {
		return herdrsetup.Options{}, err
	}
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		binary = resolved
	}
	return herdrsetup.Options{
		Files:  files,
		Binary: binary,
		Herdr:  os.Getenv("GOAGENTIC_HERDR"),
		Out:    cmd.OutOrStdout(),
	}, nil
}
