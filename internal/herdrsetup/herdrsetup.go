// Package herdrsetup installs, updates and removes the goagentic.dash Herdr
// plugin on this machine. The plugin's files are embedded in the goagentic
// binary, so the plugin always matches the binary that installed it.
package herdrsetup

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PluginID is the plugin's Herdr id; its actions are PluginID + ".<action>".
const PluginID = "goagentic.dash"

// DefaultKey opens the dashboard; DefaultFleetKey opens the fleet view.
const (
	DefaultKey      = "prefix+a"
	DefaultFleetKey = "prefix+shift+a"
)

// MinHerdr is the oldest Herdr with the plugin features the dashboard uses.
var MinHerdr = [3]int{0, 9, 0}

// Options control an install.
type Options struct {
	// Files holds the plugin files (herdr-plugin.toml, goagentic-herdr.sh).
	Files fs.FS
	// Binary is the goagentic executable the plugin should run.
	Binary string
	// Key to bind to the dashboard; empty skips the keybinding.
	Key string
	// FleetKey to bind to the fleet view; empty skips it.
	FleetKey string
	// Herdr is the herdr executable (default "herdr" on PATH).
	Herdr string
	// DataDir holds the plugin files (default ~/.local/share/goagentic/herdr-plugin).
	DataDir string
	// ConfigPath is Herdr's config.toml (default: Herdr's usual location).
	ConfigPath string
	Out        io.Writer
	Now        time.Time
}

func (o *Options) defaults() error {
	if o.Herdr == "" {
		o.Herdr = "herdr"
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if o.DataDir == "" {
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" {
			base = filepath.Join(home, ".local", "share")
		}
		o.DataDir = filepath.Join(base, "goagentic", "herdr-plugin")
	}
	if o.ConfigPath == "" {
		o.ConfigPath = ConfigPath(home)
	}
	return nil
}

// ConfigPath is where Herdr reads config.toml.
func ConfigPath(home string) string {
	if p := os.Getenv("HERDR_CONFIG_PATH"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "herdr", "config.toml")
}

// Install puts the plugin in place and registers it, then binds the key.
// Running it again updates the plugin files and leaves an existing binding.
func Install(o Options) error {
	if err := o.defaults(); err != nil {
		return err
	}
	step := func(format string, args ...any) { fmt.Fprintf(o.Out, "  ✓ "+format+"\n", args...) }

	version, err := herdrVersion(o.Herdr)
	if err != nil {
		return err
	}
	if !atLeast(version, MinHerdr) {
		return fmt.Errorf("Herdr %d.%d.%d or newer is needed (found %d.%d.%d); run `herdr update`",
			MinHerdr[0], MinHerdr[1], MinHerdr[2], version[0], version[1], version[2])
	}
	step("Herdr %d.%d.%d", version[0], version[1], version[2])

	if err := writePlugin(o.Files, o.DataDir, o.Binary); err != nil {
		return err
	}
	step("plugin files in %s", o.DataDir)

	if err := register(o.Herdr, o.DataDir); err != nil {
		return err
	}
	step("plugin %s registered with Herdr", PluginID)

	for _, b := range []struct{ key, action, what string }{
		{o.Key, "board", "the dashboard"},
		{o.FleetKey, "fleet", "the fleet view"},
	} {
		if b.key == "" {
			step("%s keybinding skipped", b.what)
			continue
		}
		added, err := bindKey(o, b.key, b.action, "agents "+strings.TrimPrefix(b.what, "the "))
		if err != nil {
			return err
		}
		if added {
			step("%s opens %s (%s)", b.key, b.what, o.ConfigPath)
		} else {
			step("%s keybinding already in %s", b.what, o.ConfigPath)
		}
	}

	// Reloading applies the key; it fails harmlessly when no server runs.
	if exec.Command(o.Herdr, "server", "reload-config").Run() == nil {
		step("Herdr config reloaded")
	}
	// Fill the sidebar summaries now rather than at the next agent event.
	_ = exec.Command(o.Herdr, "plugin", "action", "invoke", PluginID+".refresh").Run()
	return nil
}

// Uninstall unregisters the plugin, removes the keybinding it added, and
// deletes its files.
func Uninstall(o Options) error {
	if err := o.defaults(); err != nil {
		return err
	}
	step := func(format string, args ...any) { fmt.Fprintf(o.Out, "  ✓ "+format+"\n", args...) }
	if p, _ := findPlugin(o.Herdr); p != nil {
		if err := unregister(o.Herdr, p); err != nil {
			return err
		}
		step("plugin %s removed from Herdr", PluginID)
	}
	removed, err := unbindKey(o)
	if err != nil {
		return err
	}
	if removed {
		step("keybinding removed from %s", o.ConfigPath)
		_ = exec.Command(o.Herdr, "server", "reload-config").Run()
	}
	if err := os.RemoveAll(o.DataDir); err != nil {
		return err
	}
	step("plugin files removed")
	return nil
}

// Status describes the install in a few lines.
func Status(o Options) (string, error) {
	if err := o.defaults(); err != nil {
		return "", err
	}
	var b strings.Builder
	if v, err := herdrVersion(o.Herdr); err != nil {
		fmt.Fprintf(&b, "Herdr:      not found (%v)\n", err)
	} else {
		fmt.Fprintf(&b, "Herdr:      %d.%d.%d\n", v[0], v[1], v[2])
	}
	if p, _ := findPlugin(o.Herdr); p != nil {
		fmt.Fprintf(&b, "Plugin:     %s (%s: %s)\n", PluginID, p.Source.Kind, p.Root)
	} else {
		fmt.Fprintf(&b, "Plugin:     not installed\n")
	}
	cfg, _ := os.ReadFile(o.ConfigPath)
	for _, k := range []struct{ label, action string }{{"Key:", "board"}, {"Fleet key:", "fleet"}} {
		key := boundKey(string(cfg), k.action)
		if key == "" {
			key = "none"
		}
		fmt.Fprintf(&b, "%-12s%s\n", k.label, key)
	}
	return b.String(), nil
}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

func herdrVersion(bin string) ([3]int, error) {
	var v [3]int
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return v, errors.New("herdr is not installed or not on PATH (https://herdr.dev)")
		}
		return v, fmt.Errorf("herdr --version: %w", err)
	}
	m := versionRe.FindStringSubmatch(string(out))
	if m == nil {
		return v, fmt.Errorf("cannot read the Herdr version from %q", strings.TrimSpace(string(out)))
	}
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, nil
}

func atLeast(v, min [3]int) bool {
	for i := range v {
		if v[i] != min[i] {
			return v[i] > min[i]
		}
	}
	return true
}

// writePlugin replaces the plugin files and records which binary to run.
func writePlugin(files fs.FS, dir, binary string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"herdr-plugin.toml", "goagentic-herdr.sh"} {
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return fmt.Errorf("embedded plugin file %s: %w", name, err)
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, mode); err != nil {
			return err
		}
	}
	// The launcher runs this binary first, so the plugin always uses the
	// goagentic that installed it, wherever it lives.
	return os.WriteFile(filepath.Join(dir, "goagentic-path"), []byte(binary+"\n"), 0o644)
}

type pluginInfo struct {
	ID     string `json:"plugin_id"`
	Root   string `json:"plugin_root"`
	Source struct {
		Kind string `json:"kind"`
	} `json:"source"`
}

func findPlugin(bin string) (*pluginInfo, error) {
	out, err := exec.Command(bin, "plugin", "list", "--json").Output()
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Plugins []pluginInfo `json:"plugins"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, err
	}
	for _, p := range resp.Result.Plugins {
		if p.ID == PluginID {
			return &p, nil
		}
	}
	return nil, nil
}

func unregister(bin string, p *pluginInfo) error {
	verb := "unlink"
	if p.Source.Kind != "local" {
		verb = "uninstall"
	}
	if out, err := exec.Command(bin, "plugin", verb, PluginID).CombinedOutput(); err != nil {
		return fmt.Errorf("herdr plugin %s: %s", verb, strings.TrimSpace(string(out)))
	}
	return nil
}

// register links dir as the plugin, replacing any other registration (a
// development link or a GitHub install). Relinking also reloads the
// manifest after an update.
func register(bin, dir string) error {
	if p, err := findPlugin(bin); err == nil && p != nil {
		if err := unregister(bin, p); err != nil {
			return err
		}
	}
	if out, err := exec.Command(bin, "plugin", "link", dir).CombinedOutput(); err != nil {
		return fmt.Errorf("herdr plugin link: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

var keyBlockRe = regexp.MustCompile(`(?m)^\[\[keys\.command\]\]\n(?:[^\[\n].*\n?)*`)

// boundKey returns the key bound to the plugin's action, if one is.
func boundKey(cfg, action string) string {
	for _, block := range keyBlockRe.FindAllString(cfg, -1) {
		if strings.Contains(block, `"`+PluginID+`.`+action+`"`) {
			if m := regexp.MustCompile(`(?m)^key\s*=\s*"([^"]+)"`).FindStringSubmatch(block); m != nil {
				return m[1]
			}
		}
	}
	return ""
}

// keyTaken reports whether key is already bound to something else.
func keyTaken(cfg, key string) bool {
	return regexp.MustCompile(`(?m)^key\s*=\s*"` + regexp.QuoteMeta(key) + `"`).MatchString(cfg)
}

// bindKey appends a keybinding for the plugin's action unless it already
// has one. It backs the file up first and restores it if Herdr rejects the
// result.
func bindKey(o Options, key, action, description string) (bool, error) {
	cfg, err := os.ReadFile(o.ConfigPath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if boundKey(string(cfg), action) != "" {
		return false, nil
	}
	if keyTaken(string(cfg), key) {
		flag := "--key"
		if action == "fleet" {
			flag = "--fleet-key"
		}
		return false, fmt.Errorf("%s is already bound in %s; choose another with %s, or --no-key", key, o.ConfigPath, flag)
	}
	block := fmt.Sprintf("\n[[keys.command]]\nkey = %q\ntype = \"plugin_action\"\ncommand = %q\ndescription = %q\n",
		key, PluginID+"."+action, description)
	next := string(cfg)
	if next != "" && !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	next += block
	return true, writeChecked(o, cfg, []byte(next))
}

// unbindKey removes the dashboard's keybinding blocks.
func unbindKey(o Options) (bool, error) {
	cfg, err := os.ReadFile(o.ConfigPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	next := keyBlockRe.ReplaceAllStringFunc(string(cfg), func(block string) string {
		if strings.Contains(block, `"`+PluginID+`.`) {
			return ""
		}
		return block
	})
	next = regexp.MustCompile(`\n{3,}`).ReplaceAllString(next, "\n\n")
	if next == string(cfg) {
		return false, nil
	}
	return true, writeChecked(o, cfg, []byte(next))
}

// writeChecked backs up the old config, writes the new one, and restores the
// backup if `herdr config check` rejects it.
func writeChecked(o Options, old, next []byte) error {
	if err := os.MkdirAll(filepath.Dir(o.ConfigPath), 0o755); err != nil {
		return err
	}
	if len(old) > 0 {
		// Never overwrite an earlier backup, even one from the same second.
		backup := o.ConfigPath + ".bak-" + o.Now.Format("20060102-150405")
		for n := 2; fileExists(backup); n++ {
			backup = fmt.Sprintf("%s.bak-%s-%d", o.ConfigPath, o.Now.Format("20060102-150405"), n)
		}
		if err := os.WriteFile(backup, old, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(o.Out, "  ✓ backed up %s\n", backup)
	}
	if err := os.WriteFile(o.ConfigPath, next, 0o644); err != nil {
		return err
	}
	cmd := exec.Command(o.Herdr, "config", "check")
	cmd.Env = append(os.Environ(), "HERDR_CONFIG_PATH="+o.ConfigPath)
	out, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(out, []byte("ok")) {
		_ = os.WriteFile(o.ConfigPath, old, 0o644)
		return fmt.Errorf("Herdr rejected the new config, so it was left unchanged: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// MoveTab moves a tab to a position in its Space's tab row (0 is leftmost)
// through Herdr's socket API; the herdr CLI has no command for it.
func MoveTab(socket, tabID string, index int) error {
	if socket == "" {
		return errors.New("HERDR_SOCKET_PATH is not set")
	}
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req, err := json.Marshal(map[string]any{
		"id":     "goagentic-move-tab",
		"method": "tab.move",
		"params": map[string]any{"tab_id": tabID, "insert_index": index},
	})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return err
	}
	var resp struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("tab.move: unreadable reply: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("tab.move: %s: %s", resp.Error.Code, resp.Error.Message)
	}
	return nil
}
