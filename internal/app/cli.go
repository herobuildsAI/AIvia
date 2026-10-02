package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type CLIStatus struct {
	Provider  string `json:"provider"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Detail    string `json:"detail"`
}

func isCLIProvider(provider string) bool { return provider == "claude-cli" || provider == "codex-cli" }

func validateCLIPath(path string) error {
	if path != "" && (!filepath.IsAbs(path) || len(path) > 4096 || strings.ContainsAny(path, "\x00\r\n")) {
		return errors.New("Choose a trusted CLI executable using its absolute path, without command arguments.")
	}
	return nil
}

func resolveCLI(provider, path string) (string, error) {
	if err := validateCLIPath(path); err != nil {
		return "", err
	}
	if path == "" {
		var err error
		path, err = exec.LookPath(strings.TrimSuffix(provider, "-cli"))
		if err != nil || !filepath.IsAbs(path) {
			return "", errors.New("CLI was not found on PATH. Select its installed executable using an absolute path.")
		}
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0) {
		return "", errors.New("The selected CLI executable is missing or is not executable.")
	}
	return path, nil
}

const codexIsolationUnavailable = "Codex CLI analysis is unavailable: this adapter has no verified configuration that disables every model tool. A read-only sandbox alone still permits reads. No context was sent."

var cliVersion = regexp.MustCompile(`^(?:codex-cli )?([0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,5})(?: \(Claude Code\))?$`)
var cliModel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// DiscoverCLIs is called only by an explicit detection request. It invokes only
// version/help, never login, account status, a model request, or a configuration dump.
func DiscoverCLIs(ctx context.Context) []CLIStatus {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return []CLIStatus{inspectCLI(ctx, "claude-cli", ""), inspectCLI(ctx, "codex-cli", "")}
}

func inspectCLI(ctx context.Context, provider, selectedPath string) CLIStatus {
	status := CLIStatus{Provider: provider}
	path, err := resolveCLI(provider, selectedPath)
	if err != nil {
		status.Detail = err.Error()
		return status
	}
	status.Path = path
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := runCLI(ctx, path, []string{"--version"}, nil)
	if err != nil {
		status.Detail = err.Error()
		return status
	}
	match := cliVersion.FindStringSubmatch(strings.TrimSpace(string(output)))
	if match == nil {
		status.Detail = "CLI version output was not recognized; analysis is disabled."
		return status
	}
	status.Version = match[1]
	if provider == "codex-cli" {
		status.Detail = codexIsolationUnavailable
		return status
	}
	output, err = runCLI(ctx, path, []string{"--help"}, nil)
	if err != nil {
		status.Detail = err.Error()
		return status
	}
	for _, flag := range []string{"--print", "--output-format", "--tools", "--disallowedTools", "--strict-mcp-config", "--mcp-config", "--disable-slash-commands", "--settings", "--setting-sources", "--no-session-persistence", "--system-prompt", "--safe-mode", "--restricted", "--no-chrome", "--permission-mode"} {
		if !strings.Contains(string(output), flag) {
			status.Detail = "This CLI lacks required isolation options. Update it before using model chat."
			return status
		}
	}
	status.Available = true
	status.Detail = "CLI detected. Model tools and user/project customizations are disabled; administrator policies and hooks can still apply. Authentication and model access have not been checked."
	return status
}

func (c *ModelClient) cliModels(ctx context.Context) ([]Model, error) {
	status := inspectCLI(ctx, c.settings.Provider, c.settings.CLIPath)
	if !status.Available {
		return nil, errors.New(status.Detail)
	}
	models := []Model{{ID: "default", Locality: "CLI-configured provider; account/model availability not verified"}}
	if c.settings.Model != "" && c.settings.Model != "default" && cliModel.MatchString(c.settings.Model) {
		models = append(models, Model{ID: c.settings.Model, Locality: "Requested CLI model; account/model availability not verified"})
	}
	return models, nil
}

func (c *ModelClient) chatCLI(ctx context.Context, model string, messages []Message) (string, error) {
	if !cliModel.MatchString(model) {
		return "", errors.New("Use a CLI model name without paths, whitespace, or command arguments.")
	}
	history := make([]Message, 0, len(messages))
	for i, message := range messages {
		if message.Role == "system" {
			if i != 0 || message.Content != assistantInstructions {
				return "", errors.New("CLI system instructions cannot be overridden.")
			}
			continue
		}
		history = append(history, message)
	}
	if len(history) == 0 {
		return "", errors.New("A user message is required.")
	}
	if c.settings.Provider == "codex-cli" {
		return "", errors.New(codexIsolationUnavailable)
	}
	status := inspectCLI(ctx, c.settings.Provider, c.settings.CLIPath)
	if !status.Available {
		return "", errors.New(status.Detail)
	}
	prompt, err := json.Marshal(struct {
		Messages []Message `json:"messages"`
	}{history})
	if err != nil {
		return "", errors.New("Invalid conversation.")
	}
	// --safe-mode preserves the CLI's own login, unlike --bare. These switches
	// remove model tools and user customizations; managed hooks remain a stated
	// trust boundary. See code.claude.com/docs/en/cli-reference and /en/hooks.
	args := []string{"-p", "--output-format", "json", "--safe-mode", "--restricted", "--tools", "", "--disallowedTools", "*", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--disable-slash-commands", "--settings", `{"disableAllHooks":true,"disableClaudeAiConnectors":true}`, "--setting-sources", "", "--no-session-persistence", "--no-chrome", "--permission-mode", "dontAsk", "--system-prompt", assistantInstructions}
	if model != "default" {
		args = append(args, "--model", model)
	}
	output, err := runCLI(ctx, status.Path, args, prompt)
	if err != nil {
		return "", err
	}
	var result struct {
		Type              string            `json:"type"`
		Subtype           string            `json:"subtype"`
		IsError           bool              `json:"is_error"`
		Result            string            `json:"result"`
		PermissionDenials []json.RawMessage `json:"permission_denials"`
	}
	if json.Unmarshal(output, &result) != nil || result.Type != "result" || result.Subtype != "success" || result.IsError || len(result.PermissionDenials) > 0 {
		return "", errors.New("CLI returned an invalid, failed, or tool-requesting response. Check the CLI in your own terminal; no raw CLI diagnostics are shown here.")
	}
	if len(result.Result) > 64<<10 || strings.TrimSpace(result.Result) == "" {
		return "", errors.New("Model response is empty or exceeds 64 KiB.")
	}
	return result.Result, nil
}

type cliOutput struct {
	bytes.Buffer
	limit    int
	overflow bool
	cancel   context.CancelFunc
	keep     bool
}

func (b *cliOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit {
		b.overflow = true
		b.cancel()
		return 0, errors.New("CLI output limit")
	}
	b.limit -= len(p)
	if b.keep {
		return b.Buffer.Write(p)
	}
	return len(p), nil
}

func runCLI(ctx context.Context, path string, args []string, input []byte) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	dir, err := os.MkdirTemp("", "aivpn-cli-")
	if err != nil {
		return nil, errors.New("Could not create a temporary CLI workspace.")
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	// Pass only the runtime environment required by the CLI's existing login.
	// Do not inspect/copy auth files, API tokens, custom provider variables, or
	// shell/Node injection settings. The CLI owns its account credentials.
	for _, key := range []string{"HOME", "PATH", "USER", "LOGNAME", "LANG", "LC_ALL", "TMPDIR", "SystemRoot", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Env = append(cmd.Env, "PWD="+dir, "DISABLE_AUTOUPDATER=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1")
	cmd.Stdin = bytes.NewReader(input)
	stdout := &cliOutput{limit: 1 << 20, cancel: cancel, keep: true}
	stderr := &cliOutput{limit: 64 << 10, cancel: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := configureCLIProcess(cmd); err != nil {
		return nil, err
	}
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	// A wrapper can exit while its descendants remain alive. Clean the process
	// group on success and failure as well as on context cancellation.
	if cmd.Process != nil {
		_ = cmd.Cancel()
	}
	if stdout.overflow || stderr.overflow {
		return nil, errors.New("CLI output exceeded the size limit; the process was stopped.")
	}
	if err != nil {
		return nil, errors.New("CLI failed, was canceled, or timed out. Verify installation and sign-in in your own terminal; raw diagnostics are withheld.")
	}
	return stdout.Bytes(), nil
}
