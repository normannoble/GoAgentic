package tests

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	agentframework "github.com/normannoble/GoAgentic"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate bootstrap test source")
	}
	return filepath.Dir(filepath.Dir(filename))
}

func environmentWith(overrides ...string) []string {
	blocked := map[string]bool{
		"AGENT_FRAMEWORK_BINARY":       true,
		"AGENT_FRAMEWORK_INSTALL_URL":  true,
		"AGENT_FRAMEWORK_RELEASE_BASE": true,
		"AGENT_FRAMEWORK_REPOSITORY":   true,
		"AGENT_FRAMEWORK_VERSION":      true,
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !blocked[key] {
			environment = append(environment, entry)
		}
	}
	return append(environment, overrides...)
}

func writeFakeBinary(t *testing.T, path, outputPath string) []byte {
	t.Helper()
	content := []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$AGENT_FRAMEWORK_TEST_OUTPUT\"\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	if outputPath != "" {
		t.Setenv("AGENT_FRAMEWORK_TEST_OUTPUT", outputPath)
	}
	return content
}

func runShell(t *testing.T, directory, script string, environment []string, args ...string) (string, error) {
	t.Helper()
	commandArgs := append([]string{script}, args...)
	command := exec.Command("sh", commandArgs...)
	command.Dir = directory
	command.Env = environment
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	return output.String(), err
}

func requireDownloadTools(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not available")
	}
	if _, err := exec.LookPath("sha256sum"); err == nil {
		return
	}
	if _, err := exec.LookPath("shasum"); err != nil {
		t.Skip("sha256sum and shasum are not available")
	}
}

func platformAsset(t *testing.T, version string) string {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("bootstrap does not support %s", runtime.GOOS)
	}
	if runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64" {
		t.Skipf("bootstrap does not support %s", runtime.GOARCH)
	}
	return fmt.Sprintf("goagentic_%s_%s_%s", version, runtime.GOOS, runtime.GOARCH)
}

func releaseBaseURL(t *testing.T, directory string) string {
	t.Helper()
	return (&url.URL{Scheme: "file", Path: directory}).String()
}

func TestShellLaunchersAreValidPOSIXShell(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"install.sh", "init.sh", "setup.sh"} {
		t.Run(name, func(t *testing.T) {
			command := exec.Command("sh", "-n", filepath.Join(root, name))
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("sh -n failed: %v\n%s", err, output)
			}
		})
	}
}

func TestLocalBinaryOverrideCopiesAndForwardsArguments(t *testing.T) {
	root := repositoryRoot(t)
	temporary := t.TempDir()
	target := filepath.Join(temporary, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeBinary := filepath.Join(temporary, "agent framework local")
	argumentsFile := filepath.Join(temporary, "arguments.txt")
	writeFakeBinary(t, fakeBinary, argumentsFile)

	environment := environmentWith(
		"AGENT_FRAMEWORK_BINARY="+fakeBinary,
		"AGENT_FRAMEWORK_TEST_OUTPUT="+argumentsFile,
	)
	output, err := runShell(
		t,
		target,
		filepath.Join(root, "install.sh"),
		environment,
		target,
		"--principal",
		"Release Test",
		"--yes",
	)
	if err != nil {
		t.Fatalf("local override failed: %v\n%s", err, output)
	}

	arguments, err := os.ReadFile(argumentsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "init\n" + target + "\n--principal\nRelease Test\n--yes\n"
	if string(arguments) != want {
		t.Fatalf("forwarded arguments mismatch\nwant: %q\n got: %q", want, arguments)
	}
	info, err := os.Stat(fakeBinary)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("local override permissions were mutated: %o", info.Mode().Perm())
	}
}

func TestBootstrapDownloadsExpectedPlatformAssetAndVerifiesChecksum(t *testing.T) {
	requireDownloadTools(t)
	root := repositoryRoot(t)
	temporary := t.TempDir()
	releaseDirectory := filepath.Join(temporary, "release assets")
	if err := os.Mkdir(releaseDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	version := "9.8.7"
	asset := platformAsset(t, version)
	assetPath := filepath.Join(releaseDirectory, asset)
	argumentsFile := filepath.Join(temporary, "downloaded-arguments.txt")
	content := writeFakeBinary(t, assetPath, argumentsFile)
	digest := sha256.Sum256(content)
	checksum := fmt.Sprintf("%x  %s\n", digest, asset)
	if err := os.WriteFile(assetPath+".sha256", []byte(checksum), 0o644); err != nil {
		t.Fatal(err)
	}

	environment := environmentWith(
		"AGENT_FRAMEWORK_VERSION="+version,
		"AGENT_FRAMEWORK_RELEASE_BASE="+releaseBaseURL(t, releaseDirectory),
		"AGENT_FRAMEWORK_TEST_OUTPUT="+argumentsFile,
	)
	output, err := runShell(
		t,
		temporary,
		filepath.Join(root, "install.sh"),
		environment,
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("download bootstrap failed: %v\n%s", err, output)
	}
	arguments, err := os.ReadFile(argumentsFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(arguments) != "init\n--dry-run\n" {
		t.Fatalf("unexpected downloaded binary arguments: %q", arguments)
	}
}

func TestBootstrapRejectsInvalidChecksumBeforeExecution(t *testing.T) {
	requireDownloadTools(t)
	root := repositoryRoot(t)
	temporary := t.TempDir()
	releaseDirectory := filepath.Join(temporary, "release")
	if err := os.Mkdir(releaseDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	version := "9.8.7"
	asset := platformAsset(t, version)
	assetPath := filepath.Join(releaseDirectory, asset)
	argumentsFile := filepath.Join(temporary, "must-not-exist.txt")
	writeFakeBinary(t, assetPath, argumentsFile)
	checksum := strings.Repeat("0", 64) + "  " + asset + "\n"
	if err := os.WriteFile(assetPath+".sha256", []byte(checksum), 0o644); err != nil {
		t.Fatal(err)
	}

	environment := environmentWith(
		"AGENT_FRAMEWORK_VERSION="+version,
		"AGENT_FRAMEWORK_RELEASE_BASE="+releaseBaseURL(t, releaseDirectory),
		"AGENT_FRAMEWORK_TEST_OUTPUT="+argumentsFile,
	)
	output, err := runShell(
		t,
		temporary,
		filepath.Join(root, "install.sh"),
		environment,
	)
	if err == nil {
		t.Fatal("bootstrap accepted an invalid checksum")
	}
	if !strings.Contains(output, "binary checksum verification failed") {
		t.Fatalf("missing checksum error: %s", output)
	}
	if _, statErr := os.Stat(argumentsFile); !os.IsNotExist(statErr) {
		t.Fatalf("binary executed before checksum rejection: %v", statErr)
	}
}

func TestPipedCompatibilityWrapperLoadsCanonicalInstaller(t *testing.T) {
	requireDownloadTools(t)
	root := repositoryRoot(t)
	temporary := t.TempDir()
	fakeBinary := filepath.Join(temporary, "local-goagentic")
	argumentsFile := filepath.Join(temporary, "wrapper-arguments.txt")
	writeFakeBinary(t, fakeBinary, argumentsFile)
	initScript, err := os.ReadFile(filepath.Join(root, "init.sh"))
	if err != nil {
		t.Fatal(err)
	}

	installURL := releaseBaseURL(t, root) + "/install.sh"
	command := exec.Command("sh", "-s", "--", "--yes")
	command.Dir = temporary
	command.Stdin = bytes.NewReader(initScript)
	command.Env = environmentWith(
		"AGENT_FRAMEWORK_BINARY="+fakeBinary,
		"AGENT_FRAMEWORK_INSTALL_URL="+installURL,
		"AGENT_FRAMEWORK_TEST_OUTPUT="+argumentsFile,
	)
	if output, runErr := command.CombinedOutput(); runErr != nil {
		t.Fatalf("piped compatibility wrapper failed: %v\n%s", runErr, output)
	}
	arguments, err := os.ReadFile(argumentsFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(arguments) != "init\n--yes\n" {
		t.Fatalf("compatibility wrapper lost arguments: %q", arguments)
	}
}

func TestCheckoutSetupBuildsWithModulesAndPreservesCallerDirectory(t *testing.T) {
	root := repositoryRoot(t)
	temporary := t.TempDir()
	fakeTools := filepath.Join(temporary, "bin")
	callerDirectory := filepath.Join(temporary, "caller")
	if err := os.Mkdir(fakeTools, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(callerDirectory, 0o755); err != nil {
		t.Fatal(err)
	}

	goLog := filepath.Join(temporary, "go.log")
	cliLog := filepath.Join(temporary, "cli.log")
	builtTemplate := filepath.Join(temporary, "built-template")
	builtScript := []byte("#!/bin/sh\n{\n    pwd\n    printf '%s\\n' \"$@\"\n} > \"$AGENT_FRAMEWORK_TEST_OUTPUT\"\n")
	if err := os.WriteFile(builtTemplate, builtScript, 0o600); err != nil {
		t.Fatal(err)
	}

	fakeGo := filepath.Join(fakeTools, "go")
	fakeGoScript := []byte(`#!/bin/sh
{
    echo "GO111MODULE=${GO111MODULE-}"
    echo "CGO_ENABLED=${CGO_ENABLED-}"
    printf '%s\n' "$@"
} > "$AGENT_FRAMEWORK_TEST_GO_LOG"
output=
while [ "$#" -gt 0 ]; do
    if [ "$1" = "-o" ]; then
        shift
        output=$1
        break
    fi
    shift
done
test -n "$output"
cp "$AGENT_FRAMEWORK_TEST_BUILT_TEMPLATE" "$output"
chmod 0755 "$output"
`)
	if err := os.WriteFile(fakeGo, fakeGoScript, 0o755); err != nil {
		t.Fatal(err)
	}

	environment := environmentWith(
		"PATH="+fakeTools+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AGENT_FRAMEWORK_TEST_GO_LOG="+goLog,
		"AGENT_FRAMEWORK_TEST_BUILT_TEMPLATE="+builtTemplate,
		"AGENT_FRAMEWORK_TEST_OUTPUT="+cliLog,
	)
	output, err := runShell(
		t,
		callerDirectory,
		filepath.Join(root, "setup.sh"),
		environment,
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("checkout setup failed: %v\n%s", err, output)
	}

	buildInvocation, err := os.ReadFile(goLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"GO111MODULE=on\n",
		"CGO_ENABLED=0\n",
		"build\n",
		"-trimpath\n",
		"./cmd/goagentic\n",
	} {
		if !strings.Contains(string(buildInvocation), expected) {
			t.Errorf("go invocation is missing %q: %s", expected, buildInvocation)
		}
	}
	cliInvocation, err := os.ReadFile(cliLog)
	if err != nil {
		t.Fatal(err)
	}
	resolvedCaller, err := filepath.EvalSymlinks(callerDirectory)
	if err != nil {
		t.Fatal(err)
	}
	wantCLI := resolvedCaller + "\ninit\n--dry-run\n"
	if string(cliInvocation) != wantCLI {
		t.Fatalf("setup changed the CLI working directory or arguments\nwant: %q\n got: %q", wantCLI, cliInvocation)
	}
}

func TestDistributionContractStaysInSync(t *testing.T) {
	root := repositoryRoot(t)
	read := func(name string) string {
		t.Helper()
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(content)
	}

	installer := read("install.sh")
	setup := read("setup.sh")
	workflow := read(filepath.Join(".github", "workflows", "release.yml"))

	versionLine := "VERSION=${AGENT_FRAMEWORK_VERSION:-" + agentframework.Version + "}"
	if !strings.Contains(installer, versionLine) {
		t.Fatalf("install.sh version does not match Go version %s", agentframework.Version)
	}
	for _, fragment := range []string{
		`ASSET="goagentic_${VERSION}_${OS}_${ARCH}"`,
		`non_interactive_requested()`,
		`if non_interactive_requested "$@"; then`,
		`if [ -t 0 ] && [ -t 1 ]; then`,
		`elif [ -t 1 ]; then`,
		`<&1`,
		`elif [ -t 2 ] && [ -c /dev/tty ]; then`,
		`</dev/tty >/dev/tty`,
		`AGENT_FRAMEWORK_BINARY`,
	} {
		if !strings.Contains(installer, fragment) {
			t.Errorf("install.sh is missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		`SCRIPT_DIR=$(CDPATH='' cd`,
		"GO111MODULE=on CGO_ENABLED=0 go build",
		"./cmd/goagentic",
		`non_interactive_requested()`,
		`if non_interactive_requested "$@"; then`,
		`if [ -t 0 ] && [ -t 1 ]; then`,
		`</dev/tty >/dev/tty`,
	} {
		if !strings.Contains(setup, fragment) {
			t.Errorf("setup.sh is missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"goos: linux\n            goarch: amd64",
		"goos: linux\n            goarch: arm64",
		"goos: darwin\n            goarch: amd64",
		"goos: darwin\n            goarch: arm64",
		"CGO_ENABLED: \"0\"",
		"GO111MODULE: \"on\"",
		"go test ./...",
		"go vet ./...",
		"sha256sum \"$asset\" > \"$asset.sha256\"",
		"actions/download-artifact@v8",
		`"$binary" --version | grep -F "$version"`,
		"./install.sh \"$target\"",
	} {
		if !strings.Contains(workflow, fragment) {
			t.Errorf("release workflow is missing %q", fragment)
		}
	}
}
