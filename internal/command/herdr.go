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

	var key string
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
				opts.Key = key
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Installing the GoAgentic dashboard into Herdr:")
			if err := herdrsetup.Install(opts); err != nil {
				return err
			}
			if noKey {
				fmt.Fprintln(cmd.OutOrStdout(), "\nOpen it with: herdr plugin action invoke goagentic.dash.board")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "\nPress %s in any Space to open the dashboard.\n", key)
			}
			return nil
		},
	}
	install.Flags().StringVar(&key, "key", herdrsetup.DefaultKey, "Key that opens the dashboard.")
	install.Flags().BoolVar(&noKey, "no-key", false, "Do not add a keybinding.")

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

	cmd.AddCommand(install, uninstall, status)
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
