package command

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func dashWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"agents/CONVENTIONS.md":    "---\nprincipal: Norman Noble\n---\n",
		"agents/Sigrid/context.md": "---\ntitle: Platform Lead\n---\n",
		"agents/Sigrid/actions.md": "Last reviewed: 2026-09-28\n\n## Open\n\n| # | Action | Owner | Priority | Status |\n|---|---|---|---|---|\n| 1 | **Ship it.** detail | Sigrid | P1 | Open |\n",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDashOnceRendersWorkspace(t *testing.T) {
	root := dashWorkspace(t)
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"dash", "--once", "--root", filepath.Join(root, "agents", "Sigrid")},
		bytes.NewBuffer(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{filepath.Base(root), "Sigrid", "P1 #1 Ship it"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "\x1b[") {
		t.Error("non-terminal output must not carry color codes")
	}
}

func TestDashSummaryAndJSON(t *testing.T) {
	root := dashWorkspace(t)
	var stdout, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"dash", "--json", "--root", root}, bytes.NewBuffer(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("json exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"name": "Sigrid"`) {
		t.Errorf("json = %s", stdout.String())
	}
}

func TestDashOutsideWorkspaceFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"dash", "--once", "--root", t.TempDir()}, bytes.NewBuffer(nil), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "no agent workspace found") {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
}
