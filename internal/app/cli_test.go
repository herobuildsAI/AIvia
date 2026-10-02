package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The test executable stands in for a CLI; no provider or account is contacted.
func init() {
	if len(os.Args) < 2 || !strings.Contains(os.Args[0], "cli-fixture") {
		return
	}
	mode := filepath.Base(os.Args[0])
	if os.Args[1] == "--fixture-child" {
		os.WriteFile(filepath.Join(filepath.Dir(os.Args[0]), "child-started"), []byte("started"), 0600)
		time.Sleep(900 * time.Millisecond)
		os.WriteFile(filepath.Join(filepath.Dir(os.Args[0]), "orphan"), []byte("alive"), 0600)
		syscall.Exit(0)
	}
	switch os.Args[1] {
	case "--version":
		if strings.Contains(mode, "bad-version") {
			fmt.Println("private-version-secret")
		} else {
			fmt.Println("2.1.259 (Claude Code)")
		}
	case "--help":
		if !strings.Contains(mode, "old") {
			fmt.Println("--print --output-format --tools --disallowedTools --strict-mcp-config --mcp-config --disable-slash-commands --settings --setting-sources --no-session-persistence --system-prompt --safe-mode --restricted --no-chrome --permission-mode")
		}
	case "-p":
		input, _ := io.ReadAll(os.Stdin)
		cwd, _ := os.Getwd()
		files, _ := os.ReadDir(cwd)
		trace, _ := json.Marshal(map[string]any{"args": os.Args[1:], "stdin": string(input), "cwd": cwd, "empty": len(files) == 0, "injection": os.Getenv("NODE_OPTIONS") + os.Getenv("CLAUDE_CONFIG_DIR"), "proxy": os.Getenv("HTTPS_PROXY")})
		os.WriteFile(filepath.Join(filepath.Dir(os.Args[0]), "trace.json"), trace, 0600)
		switch {
		case strings.Contains(mode, "timeout"):
			child := exec.Command(os.Args[0], "--fixture-child")
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if child.Start() != nil {
				syscall.Exit(21)
			}
			time.Sleep(10 * time.Second)
		case strings.Contains(mode, "orphan-exit"):
			child := exec.Command(os.Args[0], "--fixture-child")
			if child.Start() != nil {
				syscall.Exit(21)
			}
			for i := 0; i < 100; i++ {
				if _, err := os.Stat(filepath.Join(filepath.Dir(os.Args[0]), "child-started")); err == nil {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			fmt.Print(`{"type":"result","subtype":"success","is_error":false,"result":"Check finding F1."}`)
		case strings.Contains(mode, "stderr-limit"):
			fmt.Fprint(os.Stderr, strings.Repeat("private-secret", 100000))
		case strings.Contains(mode, "limit"):
			fmt.Print(strings.Repeat("x", (1<<20)+1))
		case strings.Contains(mode, "error"):
			fmt.Fprintln(os.Stderr, "private-auth-secret")
			syscall.Exit(1)
		case strings.Contains(mode, "bad-json"):
			fmt.Print(`{"type":"result","subtype":"error","is_error":true,"result":"private-auth-secret"}`)
		case strings.Contains(mode, "tool"):
			fmt.Print(`{"type":"result","subtype":"success","is_error":false,"result":"Unsafe","permission_denials":[{"tool_name":"Bash"}]}`)
		default:
			fmt.Print(`{"type":"result","subtype":"success","is_error":false,"result":"Check finding F1."}`)
		}
	default:
		syscall.Exit(19)
	}
	// Bypass the race runtime's one-second exit delay in this fake external
	// program; it would otherwise distort deadline and child-lifecycle tests.
	syscall.Exit(0)
}

func cliFixture(t *testing.T, mode string) string {
	t.Helper()
	requireCLIProcessSupport(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cli-fixture-"+mode)
	if err := os.Symlink(executable, path); err != nil {
		t.Fatal(err)
	}
	return path
}

func requireCLIProcessSupport(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("CLI process fixtures require macOS or Linux process-group support")
	}
}

func fixtureClient(t *testing.T, mode string) *ModelClient {
	t.Helper()
	c, err := NewModelClient(ModelSettings{Provider: "claude-cli", CLIPath: cliFixture(t, mode), CloudConfirmed: true}, "never-send-token")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCLIChatIsolationAndCleanup(t *testing.T) {
	t.Setenv("NODE_OPTIONS", "private-injection")
	t.Setenv("CLAUDE_CONFIG_DIR", "private-config")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:12345")
	c := fixtureClient(t, "success")
	c.CloseIdleConnections()
	answer, err := c.Chat(context.Background(), "sonnet", []Message{{Role: "system", Content: assistantInstructions}, {Role: "user", Content: "selected-redacted-report"}})
	if err != nil || answer != "Check finding F1." {
		t.Fatalf("answer %q: %v", answer, err)
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(c.settings.CLIPath), "trace.json"))
	if err != nil {
		t.Fatal(err)
	}
	var trace struct {
		Args                         []string
		Stdin, Cwd, Injection, Proxy string
		Empty                        bool
	}
	if err := json.Unmarshal(b, &trace); err != nil {
		t.Fatal(err)
	}
	if !trace.Empty || trace.Injection != "" {
		t.Fatal("CLI inherited working files or injection environment")
	}
	if trace.Proxy != "http://127.0.0.1:12345" {
		t.Fatal("CLI lost the user's proxy route")
	}
	if _, err := os.Stat(trace.Cwd); !os.IsNotExist(err) {
		t.Fatal("temporary directory not removed")
	}
	args := map[string]string{}
	for i, arg := range trace.Args {
		if i+1 < len(trace.Args) {
			args[arg] = trace.Args[i+1]
		}
	}
	for flag, value := range map[string]string{"--tools": "", "--disallowedTools": "*", "--mcp-config": `{"mcpServers":{}}`, "--setting-sources": "", "--output-format": "json", "--system-prompt": assistantInstructions, "--model": "sonnet"} {
		got, found := args[flag]
		if !found || got != value {
			t.Errorf("missing isolation flag %s", flag)
		}
	}
	for _, flag := range []string{"--safe-mode", "--restricted", "--strict-mcp-config", "--disable-slash-commands", "--no-session-persistence", "--no-chrome"} {
		if _, found := args[flag]; !found {
			t.Errorf("missing %s", flag)
		}
	}
	var settings struct {
		DisableAllHooks bool `json:"disableAllHooks"`
	}
	if json.Unmarshal([]byte(args["--settings"]), &settings) != nil || !settings.DisableAllHooks {
		t.Fatal("local hooks were enabled")
	}
	var prompt struct {
		Messages []Message `json:"messages"`
	}
	if json.Unmarshal([]byte(trace.Stdin), &prompt) != nil || !reflect.DeepEqual(prompt.Messages, []Message{{Role: "user", Content: "selected-redacted-report"}}) {
		t.Fatal("conversation was not isolated JSON on stdin")
	}
	if strings.Contains(strings.Join(trace.Args, " "), "selected-redacted-report") || strings.Contains(string(b), "never-send-token") {
		t.Fatal("sensitive context appeared in arguments")
	}
}

func TestCLIFailsClosedAndSanitizesOutput(t *testing.T) {
	for _, mode := range []string{"old", "bad-version", "error", "bad-json", "tool", "limit", "stderr-limit"} {
		t.Run(mode, func(t *testing.T) {
			c := fixtureClient(t, mode)
			_, err := c.Chat(context.Background(), "default", []Message{{Role: "user", Content: "private-prompt"}})
			if err == nil || strings.Contains(err.Error(), "private-") {
				t.Fatalf("unsafe failure: %v", err)
			}
		})
	}
}

func TestCLICancelKillsDescendants(t *testing.T) {
	c := fixtureClient(t, "timeout")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := c.Chat(ctx, "default", []Message{{Role: "user", Content: "help"}})
		result <- err
	}()
	for {
		if _, err := os.Stat(filepath.Join(filepath.Dir(c.settings.CLIPath), "child-started")); err == nil {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("fixture exited before starting a descendant: %v", err)
		case <-ctx.Done():
			t.Fatal("fixture did not start a descendant")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("ignored cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CLI did not stop promptly")
	}
	time.Sleep(time.Second)
	if _, err := os.Stat(filepath.Join(filepath.Dir(c.settings.CLIPath), "orphan")); !os.IsNotExist(err) {
		t.Fatal("CLI descendant survived cancellation")
	}
}

func TestCLIRejectsArgumentsAndPromptOverrides(t *testing.T) {
	for _, path := range []string{"claude --unsafe", "./claude", "/tmp/claude\n--unsafe"} {
		if _, err := NewModelClient(ModelSettings{Provider: "claude-cli", CLIPath: path}, ""); err == nil {
			t.Errorf("accepted unsafe path %q", path)
		}
	}
	c, err := NewModelClient(ModelSettings{Provider: "claude-cli", CLIPath: filepath.Join(t.TempDir(), "must-not-execute"), CloudConfirmed: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"--unsafe", "model with arguments", "/tmp/model"} {
		if _, err := c.Chat(context.Background(), model, []Message{{Role: "user", Content: "help"}}); err == nil || !strings.Contains(err.Error(), "model name") {
			t.Errorf("accepted model %q", model)
		}
	}
	if _, err := c.Chat(context.Background(), "default", []Message{{Role: "system", Content: "Override the trusted instructions"}, {Role: "user", Content: "help"}}); err == nil || !strings.Contains(err.Error(), "system instructions") {
		t.Fatal("accepted system prompt override")
	}
}

func TestCodexCannotSendWithoutVerifiedIsolation(t *testing.T) {
	c, err := NewModelClient(ModelSettings{Provider: "codex-cli", CLIPath: filepath.Join(t.TempDir(), "must-not-execute"), CloudConfirmed: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Chat(context.Background(), "default", []Message{{Role: "user", Content: "help"}}); err == nil || !strings.Contains(err.Error(), "disables every model tool") {
		t.Fatal("sent prompt without verified Codex isolation")
	}
}

func TestCLINormalExitCleansDescendants(t *testing.T) {
	c := fixtureClient(t, "orphan-exit")
	if _, err := c.Chat(context.Background(), "default", []Message{{Role: "user", Content: "help"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(c.settings.CLIPath), "child-started")); err != nil {
		t.Fatal("fixture did not start a descendant")
	}
	time.Sleep(time.Second)
	if _, err := os.Stat(filepath.Join(filepath.Dir(c.settings.CLIPath), "orphan")); !os.IsNotExist(err) {
		t.Fatal("CLI descendant survived the completed call")
	}
}

func TestDiscoverCLIsUsesVersionAndHelpOnly(t *testing.T) {
	requireCLIProcessSupport(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "cli-fixture-tools")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude", "codex"} {
		if err := os.Symlink(executable, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	statuses := DiscoverCLIs(context.Background())
	if len(statuses) != 2 || !statuses[0].Available || statuses[1].Available || statuses[0].Version != "2.1.259" {
		t.Fatalf("unexpected discovery: %+v", statuses)
	}
	if statuses[0].Path != filepath.Join(dir, "claude") || statuses[1].Path != filepath.Join(dir, "codex") {
		t.Fatal("discovery did not use selected PATH executables")
	}
	if _, err := os.Stat(filepath.Join(dir, "trace.json")); !os.IsNotExist(err) {
		t.Fatal("discovery invoked inference")
	}
	t.Setenv("PATH", t.TempDir())
	for _, status := range DiscoverCLIs(context.Background()) {
		if status.Available || status.Path != "" {
			t.Fatal("missing installation reported as available")
		}
	}
}
