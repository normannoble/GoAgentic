package command

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute(context.Background(), []string{"--version"}, bytes.NewBuffer(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if got, want := stdout.String(), "goagentic 0.1.0\n"; got != want {
		t.Fatalf("stdout = %q; want %q", got, want)
	}
}

func TestInitHelpListsDryRun(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute(context.Background(), []string{"init", "--help"}, bytes.NewBuffer(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "--dry-run") {
		t.Fatalf("help does not list --dry-run:\n%s", stdout.String())
	}
}

func TestNonInteractiveApplyRequiresYes(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute(context.Background(), []string{"init", t.TempDir()}, bytes.NewBuffer(nil), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d; want 1", code)
	}
	want := "Error: " + nonInteractiveError + "\n"
	if got := stderr.String(); got != want {
		t.Fatalf("stderr = %q; want %q", got, want)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q; want empty", stdout.String())
	}
}

func TestJSONDryRunUsesNonInteractiveDefaults(t *testing.T) {
	target := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute(
		context.Background(),
		[]string{
			"init",
			target,
			"--dry-run",
			"--json",
			"--allow-non-git",
			"--naming",
			"NORSE",
			"--no-claude",
		},
		bytes.NewBuffer(nil),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if bytes.Contains(stdout.Bytes(), []byte("\x1b")) {
		t.Fatalf("JSON contains ANSI escapes: %q", stdout.String())
	}

	var payload struct {
		Plan struct {
			Target  string `json:"target"`
			Options struct {
				Principal string `json:"principal"`
				Naming    string `json:"naming"`
				Skills    bool   `json:"skills"`
				Workspace bool   `json:"workspace"`
				Claude    bool   `json:"claude"`
			} `json:"options"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, stdout.String())
	}
	canonical, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Plan.Target != canonical {
		t.Fatalf("target = %q; want %q", payload.Plan.Target, canonical)
	}
	if payload.Plan.Options.Principal != "the principal" ||
		payload.Plan.Options.Naming != "norse" ||
		!payload.Plan.Options.Skills ||
		!payload.Plan.Options.Workspace ||
		payload.Plan.Options.Claude {
		t.Fatalf("unexpected options: %+v", payload.Plan.Options)
	}
}

func TestJSONDryRunReportsUnsafePlansWithoutWriting(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(t *testing.T, target string)
		wantAction string
	}{
		{
			name: "customized file is a conflict",
			prepare: func(t *testing.T, target string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(target, "PHILOSOPHY.md"), []byte("custom\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantAction: "conflict",
		},
		{
			name: "nonregular destination is blocked",
			prepare: func(t *testing.T, target string) {
				t.Helper()
				if err := os.Mkdir(filepath.Join(target, "PHILOSOPHY.md"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			wantAction: "blocked",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := t.TempDir()
			test.prepare(t, target)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Execute(
				context.Background(),
				[]string{
					"init", target,
					"--dry-run", "--json", "--allow-non-git",
					"--no-skills", "--no-workspace", "--no-claude",
				},
				bytes.NewBuffer(nil),
				&stdout,
				&stderr,
			)
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %q, stdout = %q", code, stderr.String(), stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q; want empty", stderr.String())
			}

			var payload struct {
				Plan struct {
					Ready   bool `json:"ready"`
					Changes []struct {
						Path   string `json:"path"`
						Action string `json:"action"`
					} `json:"changes"`
				} `json:"plan"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
				t.Fatalf("decode JSON: %v\n%s", err, stdout.String())
			}
			if payload.Plan.Ready {
				t.Fatal("unsafe dry-run plan reported ready")
			}
			found := false
			for _, change := range payload.Plan.Changes {
				if change.Path == "PHILOSOPHY.md" && change.Action == test.wantAction {
					found = true
				}
			}
			if !found {
				t.Fatalf("PHILOSOPHY.md action %q not found: %+v", test.wantAction, payload.Plan.Changes)
			}
			if _, err := os.Stat(filepath.Join(target, "CONVENTIONS.md")); !os.IsNotExist(err) {
				t.Fatalf("dry run wrote CONVENTIONS.md: %v", err)
			}
		})
	}
}

func TestYesDoesNotImplyOverwrite(t *testing.T) {
	target := t.TempDir()
	philosophy := filepath.Join(target, "PHILOSOPHY.md")
	if err := os.WriteFile(philosophy, []byte("custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute(
		context.Background(),
		[]string{
			"init",
			target,
			"--yes",
			"--allow-non-git",
			"--no-skills",
			"--no-workspace",
			"--no-claude",
		},
		bytes.NewBuffer(nil),
		&stdout,
		&stderr,
	)
	if code != 1 {
		t.Fatalf("exit code = %d; want 1", code)
	}
	if !strings.Contains(stderr.String(), "need a decision") {
		t.Fatalf("stderr does not explain the conflict: %q", stderr.String())
	}
	content, err := os.ReadFile(philosophy)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(content), "custom\n"; got != want {
		t.Fatalf("PHILOSOPHY.md = %q; want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(target, "CONVENTIONS.md")); !os.IsNotExist(err) {
		t.Fatalf("CONVENTIONS.md was written before conflict resolution; stat error = %v", err)
	}
}

func TestNonGitRequiresExplicitPermission(t *testing.T) {
	target := t.TempDir()
	var rejectedOut bytes.Buffer
	var rejectedErr bytes.Buffer
	rejected := Execute(
		context.Background(),
		[]string{"init", target, "--dry-run", "--json"},
		bytes.NewBuffer(nil),
		&rejectedOut,
		&rejectedErr,
	)
	if rejected != 1 {
		t.Fatalf("rejected exit code = %d; want 1", rejected)
	}
	if !strings.Contains(rejectedOut.String(), "--allow-non-git") {
		t.Fatalf("rejection does not mention --allow-non-git: %q", rejectedOut.String())
	}

	var acceptedOut bytes.Buffer
	var acceptedErr bytes.Buffer
	accepted := Execute(
		context.Background(),
		[]string{"init", target, "--allow-non-git", "--dry-run", "--json"},
		bytes.NewBuffer(nil),
		&acceptedOut,
		&acceptedErr,
	)
	if accepted != 0 {
		t.Fatalf("accepted exit code = %d, stderr = %q", accepted, acceptedErr.String())
	}
}

func TestJSONErrorHasStableShape(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute(
		context.Background(),
		[]string{"init", "/definitely/not/here", "--dry-run", "--json"},
		bytes.NewBuffer(nil),
		&stdout,
		&stderr,
	)
	if code != 1 {
		t.Fatalf("exit code = %d; want 1", code)
	}
	want := "{\"error\":\"Target directory does not exist: /definitely/not/here\",\"code\":1}\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q; want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q; want empty", stderr.String())
	}
}

func TestJSONCoversArgumentAndFlagParsingErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "too many arguments",
			args: []string{"init", t.TempDir(), "extra", "--dry-run", "--json"},
			want: "accepts at most 1 arg(s), received 2",
		},
		{
			name: "unknown flag",
			args: []string{"init", t.TempDir(), "--unknown", "--json=true"},
			want: "unknown flag: --unknown",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Execute(context.Background(), test.args, bytes.NewBuffer(nil), &stdout, &stderr)
			if code != 2 {
				t.Fatalf("exit code = %d; want 2", code)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q; want empty", stderr.String())
			}
			var payload struct {
				Error string `json:"error"`
				Code  int    `json:"code"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
				t.Fatalf("decode JSON: %v: %q", err, stdout.String())
			}
			if payload.Code != 2 || payload.Error != test.want {
				t.Fatalf("payload = %+v; want error %q and code 2", payload, test.want)
			}
		})
	}
}

func TestJSONPrescanHonorsValuesAndArgumentSeparator(t *testing.T) {
	if !requestsJSON([]string{"init", "--json=true"}) {
		t.Fatal("--json=true was not recognized")
	}
	if requestsJSON([]string{"init", "--json=false"}) {
		t.Fatal("--json=false enabled JSON output")
	}
	if requestsJSON([]string{"init", "--", "--json"}) {
		t.Fatal("positional --json after -- enabled JSON output")
	}
}

func TestCancelledJSONUsesExit130(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute(
		ctx,
		[]string{"init", "--dry-run", "--json"},
		bytes.NewBuffer(nil),
		&stdout,
		&stderr,
	)
	if code != 130 {
		t.Fatalf("exit code = %d; want 130", code)
	}
	if got, want := stdout.String(), "{\"error\":\"cancelled\",\"code\":130}\n"; got != want {
		t.Fatalf("stdout = %q; want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q; want empty", stderr.String())
	}
}

func TestOpposingComponentFlagsFail(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute(
		context.Background(),
		[]string{"init", t.TempDir(), "--dry-run", "--skills", "--no-skills"},
		bytes.NewBuffer(nil),
		&stdout,
		&stderr,
	)
	if code != 1 {
		t.Fatalf("exit code = %d; want 1", code)
	}
	if got, want := stderr.String(), "Error: --skills and --no-skills cannot be used together\n"; got != want {
		t.Fatalf("stderr = %q; want %q", got, want)
	}
}
