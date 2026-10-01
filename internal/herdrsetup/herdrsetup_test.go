package herdrsetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// fakeHerdr writes a herdr stand-in that logs calls, keeps one plugin
// registration in a state file, and rejects configs containing "BROKEN".
func fakeHerdr(t *testing.T, version, initialPlugin string) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "calls.log")
	state := filepath.Join(dir, "plugin.json")
	if initialPlugin != "" {
		if err := os.WriteFile(state, []byte(initialPlugin), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin = filepath.Join(dir, "herdr")
	script := `#!/bin/sh
echo "$@" >> "` + log + `"
state="` + state + `"
case "$1 $2" in
"--version ") echo "herdr ` + version + ` (abc)" ;;
"plugin list")
	if [ -f "$state" ]; then echo "{\"result\":{\"plugins\":[$(cat "$state")]}}"; else echo '{"result":{"plugins":[]}}'; fi ;;
"plugin link") echo "{\"plugin_id\":\"goagentic.dash\",\"plugin_root\":\"$3\",\"source\":{\"kind\":\"local\"}}" > "$state" ;;
"plugin unlink"|"plugin uninstall") rm -f "$state" ;;
"config check")
	if grep -q BROKEN "$HERDR_CONFIG_PATH"; then echo "config: issues found"; exit 1; fi
	echo "config: ok" ;;
esac
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

var pluginFiles = fstest.MapFS{
	"herdr-plugin.toml":  {Data: []byte("id = \"goagentic.dash\"\n")},
	"goagentic-herdr.sh": {Data: []byte("#!/bin/sh\n")},
}

func options(t *testing.T, bin string) Options {
	dir := t.TempDir()
	return Options{
		Files:      pluginFiles,
		Binary:     "/opt/goagentic",
		Key:        DefaultKey,
		Herdr:      bin,
		DataDir:    filepath.Join(dir, "plugin"),
		ConfigPath: filepath.Join(dir, "herdr", "config.toml"),
		Now:        time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC),
	}
}

func TestInstallFreshThenAgain(t *testing.T) {
	bin, log := fakeHerdr(t, "0.9.1", "")
	o := options(t, bin)
	if err := os.MkdirAll(filepath.Dir(o.ConfigPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.ConfigPath, []byte("[update]\nchannel = \"preview\""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Install(o); err != nil {
		t.Fatal(err)
	}

	if b, _ := os.ReadFile(filepath.Join(o.DataDir, "goagentic-path")); strings.TrimSpace(string(b)) != "/opt/goagentic" {
		t.Errorf("goagentic-path = %q", b)
	}
	if info, err := os.Stat(filepath.Join(o.DataDir, "goagentic-herdr.sh")); err != nil || info.Mode()&0o100 == 0 {
		t.Errorf("launcher not executable: %v", err)
	}
	cfg, _ := os.ReadFile(o.ConfigPath)
	if !strings.HasPrefix(string(cfg), "[update]\nchannel = \"preview\"\n\n[[keys.command]]") ||
		!strings.Contains(string(cfg), `command = "goagentic.dash.board"`) {
		t.Errorf("config:\n%s", cfg)
	}
	if boundKey(string(cfg), "board") != "prefix+a" {
		t.Errorf("bound key = %q", boundKey(string(cfg), "board"))
	}
	backups, _ := filepath.Glob(o.ConfigPath + ".bak-*")
	if len(backups) != 1 {
		t.Errorf("backups = %v", backups)
	}

	// A second run updates the plugin but does not add a second binding.
	if err := Install(o); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(o.ConfigPath)
	if strings.Count(string(again), "[[keys.command]]") != 1 {
		t.Errorf("second install duplicated the key:\n%s", again)
	}
	calls, _ := os.ReadFile(log)
	if n := strings.Count(string(calls), "plugin link "+o.DataDir); n != 2 {
		t.Errorf("links = %d\n%s", n, calls)
	}
	if !strings.Contains(string(calls), "plugin unlink goagentic.dash") {
		t.Errorf("reinstall should relink:\n%s", calls)
	}
}

func TestInstallReplacesGitHubInstall(t *testing.T) {
	bin, log := fakeHerdr(t, "0.9.0-preview.2026-09-09-5a24", `{"plugin_id":"goagentic.dash","plugin_root":"/managed","source":{"kind":"github"}}`)
	o := options(t, bin)
	if err := Install(o); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "plugin uninstall goagentic.dash") {
		t.Errorf("a GitHub install must be uninstalled first:\n%s", calls)
	}
}

func TestInstallRefusesOldHerdrAndTakenKey(t *testing.T) {
	bin, _ := fakeHerdr(t, "0.8.4", "")
	if err := Install(options(t, bin)); err == nil || !strings.Contains(err.Error(), "0.9.0 or newer") {
		t.Errorf("old herdr: %v", err)
	}

	bin, _ = fakeHerdr(t, "0.9.1", "")
	o := options(t, bin)
	_ = os.MkdirAll(filepath.Dir(o.ConfigPath), 0o755)
	taken := "[[keys.command]]\nkey = \"prefix+a\"\ntype = \"command\"\ncommand = \"something else\"\n"
	_ = os.WriteFile(o.ConfigPath, []byte(taken), 0o644)
	if err := Install(o); err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Errorf("taken key: %v", err)
	}
	if cfg, _ := os.ReadFile(o.ConfigPath); string(cfg) != taken {
		t.Error("config changed despite the refusal")
	}

	o.Key = "prefix+g"
	if err := Install(o); err != nil {
		t.Fatalf("another key: %v", err)
	}
}

func TestRejectedConfigIsRestored(t *testing.T) {
	bin, _ := fakeHerdr(t, "0.9.1", "")
	o := options(t, bin)
	_ = os.MkdirAll(filepath.Dir(o.ConfigPath), 0o755)
	original := "# BROKEN already\n"
	_ = os.WriteFile(o.ConfigPath, []byte(original), 0o644)
	if err := Install(o); err == nil || !strings.Contains(err.Error(), "left unchanged") {
		t.Fatalf("err = %v", err)
	}
	if cfg, _ := os.ReadFile(o.ConfigPath); string(cfg) != original {
		t.Errorf("config not restored:\n%s", cfg)
	}
}

func TestUninstall(t *testing.T) {
	bin, _ := fakeHerdr(t, "0.9.1", "")
	o := options(t, bin)
	_ = os.MkdirAll(filepath.Dir(o.ConfigPath), 0o755)
	other := "[[keys.command]]\nkey = \"prefix+l\"\ntype = \"plugin_action\"\ncommand = \"example.layout.apply\"\n"
	_ = os.WriteFile(o.ConfigPath, []byte("[update]\nchannel = \"preview\"\n\n"+other), 0o644)
	if err := Install(o); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(o); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(o.ConfigPath)
	if backups, _ := filepath.Glob(o.ConfigPath + ".bak-*"); len(backups) != 2 {
		t.Errorf("install and uninstall in the same second must keep both backups: %v", backups)
	}
	if strings.Contains(string(cfg), "goagentic.dash") || !strings.Contains(string(cfg), other) {
		t.Errorf("uninstall must remove only its own binding:\n%s", cfg)
	}
	if _, err := os.Stat(o.DataDir); !os.IsNotExist(err) {
		t.Error("plugin files left behind")
	}
	if p, _ := findPlugin(bin); p != nil {
		t.Error("plugin still registered")
	}
}

func TestHerdrMissing(t *testing.T) {
	o := options(t, filepath.Join(t.TempDir(), "no-herdr"))
	o.Herdr = "definitely-not-herdr-xyz"
	if err := Install(o); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("err = %v", err)
	}
}

func TestInstallAddsFleetKeyToExistingInstall(t *testing.T) {
	bin, _ := fakeHerdr(t, "0.9.1", "")
	o := options(t, bin)
	_ = os.MkdirAll(filepath.Dir(o.ConfigPath), 0o755)
	old := "[[keys.command]]\nkey = \"prefix+a\"\ntype = \"plugin_action\"\ncommand = \"goagentic.dash.board\"\ndescription = \"agents dashboard\"\n"
	_ = os.WriteFile(o.ConfigPath, []byte(old), 0o644)
	o.FleetKey = DefaultFleetKey
	for i := 0; i < 2; i++ {
		if err := Install(o); err != nil {
			t.Fatal(err)
		}
	}
	cfg, _ := os.ReadFile(o.ConfigPath)
	if !strings.HasPrefix(string(cfg), old) || strings.Count(string(cfg), "[[keys.command]]") != 2 {
		t.Errorf("want the old binding kept and one fleet binding added:\n%s", cfg)
	}
	if boundKey(string(cfg), "fleet") != "prefix+shift+a" || boundKey(string(cfg), "board") != "prefix+a" {
		t.Errorf("keys: board %q fleet %q", boundKey(string(cfg), "board"), boundKey(string(cfg), "fleet"))
	}
	if err := Uninstall(o); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := os.ReadFile(o.ConfigPath); strings.Contains(string(cfg), "goagentic.dash") {
		t.Errorf("uninstall left a binding:\n%s", cfg)
	}
}
