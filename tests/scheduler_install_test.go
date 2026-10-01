package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// schedulerFixture is a throwaway workspace with the scheduler scripts copied in,
// a HOME of its own, and a PATH holding only fakes for the OS-specific tools
// (uname, systemctl, loginctl, crontab, launchctl) plus the coreutils the
// scripts need. Nothing touches the real timer state of the machine running
// the tests.
type schedulerFixture struct {
	workspace string
	home      string
	calls     string
	crontab   string
	env       []string
}

const fakeTool = `#!/bin/sh
printf '%s %s\n' "$(basename "$0")" "$*" >> "$FAKE_CALLS"
case "$(basename "$0")" in
  uname) printf '%s\n' "$FAKE_UNAME" ;;
  systemctl)
    case "$*" in
      *show-environment*) exit "${FAKE_SYSTEMD_RC:-0}" ;;
    esac ;;
  loginctl) printf 'Linger=%s\n' "${FAKE_LINGER:-yes}" ;;
  crontab)
    if [ "$1" = "-l" ]; then
      [ -f "$FAKE_CRONTAB" ] && cat "$FAKE_CRONTAB" && exit 0
      exit 1
    fi
    cat > "$FAKE_CRONTAB" ;;
esac
exit 0
`

func newSchedulerFixture(t *testing.T, fakes ...string) schedulerFixture {
	t.Helper()
	root := repositoryRoot(t)
	base := t.TempDir()

	workspace := filepath.Join(base, "My Workspace")
	scheduler := filepath.Join(workspace, "agents", "scheduler")
	if err := os.MkdirAll(scheduler, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"install.sh", "install-launchd.sh", "install-systemd.sh", "setup.sh", "tick.sh"} {
		content, err := os.ReadFile(filepath.Join(root, "template", "agents", "scheduler", name))
		if err != nil {
			t.Fatalf("read template %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(scheduler, name), content, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "agents", "scheduled-tasks.md"), []byte("# Scheduled tasks\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(base, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"bash", "sh", "basename", "dirname", "tr", "mkdir", "cat", "grep", "id", "rm", "printf"} {
		real, err := exec.LookPath(tool)
		if err != nil {
			continue // a builtin on this system; bash provides it
		}
		if err := os.Symlink(real, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range fakes {
		if err := os.WriteFile(filepath.Join(bin, tool), []byte(fakeTool), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	home := filepath.Join(base, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	f := schedulerFixture{
		workspace: workspace,
		home:      home,
		calls:     filepath.Join(base, "calls.log"),
		crontab:   filepath.Join(base, "crontab"),
	}
	f.env = []string{
		"PATH=" + bin,
		"HOME=" + home,
		"USER=tester",
		"FAKE_CALLS=" + f.calls,
		"FAKE_CRONTAB=" + f.crontab,
	}
	return f
}

func (f schedulerFixture) run(t *testing.T, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	command := exec.Command(bash, append([]string{filepath.Join(f.workspace, "agents", "scheduler", "install.sh")}, args...)...)
	command.Dir = f.workspace
	command.Env = append(append([]string{}, f.env...), extraEnv...)
	output, err := command.CombinedOutput()
	return string(output), err
}

func (f schedulerFixture) read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	return string(content)
}

func TestSchedulerInstallUsesLaunchdOnMacOS(t *testing.T) {
	f := newSchedulerFixture(t, "uname", "launchctl", "systemctl", "crontab")
	output, err := f.run(t, []string{"FAKE_UNAME=Darwin"})
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}
	plist := f.read(t, filepath.Join(f.home, "Library", "LaunchAgents", "com.my-workspace.agent-scheduler.plist"))
	if !strings.Contains(plist, "<integer>7</integer>") {
		t.Errorf("plist does not fire at :07:\n%s", plist)
	}
	calls := f.read(t, f.calls)
	if !strings.Contains(calls, "launchctl load -w") {
		t.Errorf("launchd job was not loaded; calls:\n%s", calls)
	}
	if strings.Contains(calls, "systemctl --user enable") || strings.Contains(calls, "crontab -\n") {
		t.Errorf("macOS install touched another backend; calls:\n%s", calls)
	}
}

func TestSchedulerInstallUsesSystemdTimerOnLinux(t *testing.T) {
	f := newSchedulerFixture(t, "uname", "systemctl", "loginctl", "crontab")
	output, err := f.run(t, []string{"FAKE_UNAME=Linux", "FAKE_SYSTEMD_RC=0"})
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}
	units := filepath.Join(f.home, ".config", "systemd", "user")
	timer := f.read(t, filepath.Join(units, "my-workspace-agent-scheduler.timer"))
	for _, want := range []string{"OnCalendar=*-*-* *:07:00", "Persistent=true", "Unit=my-workspace-agent-scheduler.service", "WantedBy=timers.target"} {
		if !strings.Contains(timer, want) {
			t.Errorf("timer is missing %q:\n%s", want, timer)
		}
	}
	service := f.read(t, filepath.Join(units, "my-workspace-agent-scheduler.service"))
	for _, want := range []string{"Type=oneshot", "WorkingDirectory=" + f.workspace, "tick.sh"} {
		if !strings.Contains(service, want) {
			t.Errorf("service is missing %q:\n%s", want, service)
		}
	}
	calls := f.read(t, f.calls)
	for _, want := range []string{"systemctl --user daemon-reload", "systemctl --user enable --now my-workspace-agent-scheduler.timer"} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing call %q; calls:\n%s", want, calls)
		}
	}
	if _, err := os.Stat(f.crontab); err == nil {
		t.Errorf("systemd install also wrote a crontab")
	}
}

func TestSchedulerInstallWarnsWhenLingerIsOff(t *testing.T) {
	f := newSchedulerFixture(t, "uname", "systemctl", "loginctl", "crontab")
	output, err := f.run(t, []string{"FAKE_UNAME=Linux", "FAKE_LINGER=no"})
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "loginctl enable-linger tester") {
		t.Errorf("no linger warning in output:\n%s", output)
	}
	if strings.Contains(f.read(t, f.calls), "sudo") {
		t.Errorf("installer must never run sudo itself")
	}
}

func TestSchedulerInstallFallsBackToCronWithoutSystemd(t *testing.T) {
	f := newSchedulerFixture(t, "uname", "systemctl", "crontab")
	output, err := f.run(t, []string{"FAKE_UNAME=Linux", "FAKE_SYSTEMD_RC=1"})
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}
	if !strings.Contains(f.read(t, f.crontab), "my-workspace-agent-scheduler") {
		t.Errorf("cron entry not written:\n%s", f.read(t, f.crontab))
	}
	if _, err := os.Stat(filepath.Join(f.home, ".config", "systemd", "user", "my-workspace-agent-scheduler.timer")); err == nil {
		t.Errorf("cron fallback also wrote a systemd timer")
	}
}

func TestSchedulerBackendOverrideWins(t *testing.T) {
	f := newSchedulerFixture(t, "uname", "systemctl", "crontab")
	output, err := f.run(t, []string{"FAKE_UNAME=Linux", "FAKE_SYSTEMD_RC=0", "AGENT_SCHEDULER_BACKEND=cron"})
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}
	if !strings.Contains(f.read(t, f.crontab), "my-workspace-agent-scheduler") {
		t.Errorf("override to cron was ignored")
	}
}

func TestSchedulerInstallRejectsUnknownBackendOverride(t *testing.T) {
	f := newSchedulerFixture(t, "uname", "systemctl", "crontab")
	output, err := f.run(t, []string{"FAKE_UNAME=Linux", "AGENT_SCHEDULER_BACKEND=anacron"})
	if err == nil {
		t.Fatalf("expected failure for unknown backend, got:\n%s", output)
	}
	if !strings.Contains(output, "anacron") {
		t.Errorf("error does not name the bad backend:\n%s", output)
	}
}

func TestSchedulerWhichReportsWithoutInstalling(t *testing.T) {
	cases := []struct {
		name string
		env  []string
		want string
	}{
		{"macOS", []string{"FAKE_UNAME=Darwin"}, "launchd"},
		{"Linux with systemd", []string{"FAKE_UNAME=Linux", "FAKE_SYSTEMD_RC=0"}, "systemd"},
		{"Linux without systemd", []string{"FAKE_UNAME=Linux", "FAKE_SYSTEMD_RC=1"}, "cron"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newSchedulerFixture(t, "uname", "systemctl", "launchctl", "crontab")
			output, err := f.run(t, c.env, "--which")
			if err != nil {
				t.Fatalf("--which failed: %v\n%s", err, output)
			}
			if strings.TrimSpace(output) != c.want {
				t.Errorf("--which = %q, want %q", strings.TrimSpace(output), c.want)
			}
			calls, _ := os.ReadFile(f.calls)
			for _, forbidden := range []string{"launchctl load", "systemctl --user enable", "crontab -\n", "daemon-reload"} {
				if strings.Contains(string(calls), forbidden) {
					t.Errorf("--which installed something (%q); calls:\n%s", forbidden, calls)
				}
			}
		})
	}
}

func TestSchedulerInstallFailsWhenNoBackendExists(t *testing.T) {
	f := newSchedulerFixture(t, "uname")
	output, err := f.run(t, []string{"FAKE_UNAME=FreeBSD"})
	if err == nil {
		t.Fatalf("expected failure with no launchd, systemd or cron, got:\n%s", output)
	}
	if !strings.Contains(output, "FreeBSD") {
		t.Errorf("error does not name the OS:\n%s", output)
	}
}
