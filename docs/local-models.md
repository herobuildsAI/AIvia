# Assistant setup

AIvia does not install runtimes, download models, or sign in to accounts. Local-model providers use loopback HTTP endpoints. The optional Claude Code connector may send content to the CLI's configured cloud provider after explicit consent. Codex uses a manual clipboard handoff.

## Ollama

Use the native provider with `http://127.0.0.1:11434`. Disable cloud features in the Ollama process configuration. For a manually launched server on a POSIX shell:

```sh
OLLAMA_NO_CLOUD=1 ollama serve
```

Do not launch a second server if the desktop application already owns that port. Configure and restart the existing application instead. Ollama also documents `disable_ollama_cloud` in its server configuration; check [the official local-only instructions](https://docs.ollama.com/faq#how-do-i-disable-ollama-cloud-features) and verify the runtime's cloud-disabled log message.

## Compatible servers

Choose OpenAI-compatible local server and use its loopback endpoint, such as `http://127.0.0.1:1234/v1`. The adapter calls `/v1/models` and `/v1/chat/completions`. Only non-streaming chat text is supported. The application does not implement tools, reasoning-only output, vision, remote hosts, or redirected endpoints.

If the server requires bearer authentication, supply `AIVPN_MODEL_TOKEN` in the AIvia process environment before startup. Never put a key in the endpoint URL. This token is process-wide for the configured local endpoint.

## In the interface

1. Save the connection and list models. Select or enter the exact installed model ID.
2. Independently verify that the runtime and model perform local inference with cloud routing disabled; check the confirmation box and save.
3. Open **Assistant**. **Direct chat** works without a service or report: enter a question and send it. The optional failure stage and error text help the model separate network, region-policy, session, account, rate-limit, and payment hypotheses. Remove credentials and personal information before pasting errors. These fields are not saved.
4. To attach evidence, choose **Discuss a diagnostic report**, select the service and report, and **Preview context** before sending. Stored notes remain opt-in. Direct chat never automatically attaches stored service data, even when a service is selected elsewhere.
5. **Preview context** is also available in direct mode. It shows the fixed instructions, exact supplied context, and model settings without making a model request. **New conversation** clears the message history; context or connection edits invalidate the previous conversation.

Known remote/cloud metadata and cloud-tagged Ollama model names are rejected before context is sent. Generic compatible servers cannot prove locality, so user confirmation is required. The backend ignores browser-supplied roles and creates its own bounded message sequence. Changing the service/report/model/context clears the active conversation. Error text is user-supplied evidence, not a verified diagnosis; the model is instructed to state uncertainty and suggest manual checks. Editing the provider, endpoint, or model ID also clears the previous locality confirmation. Reconfirm and save before previewing another context; controls stay locked while saving or listing models.

If another window changes the assistant connection before a new context starts, sending stops. The latest settings are displayed with consent cleared; review and save them before retrying. Your unsent question is preserved. A connection change after preview also invalidates the backend conversation.

Limits: 180 seconds per model request, 32 KiB combined input, 64 KiB output text, 1 MiB HTTP response, 20 conversation messages, and 30-minute context validity. Long chats may hit the input limit earlier. Stop cancels the request; the runtime may need time to release compute. Start a fresh context after a canceled or failed response. The runtime must support enough context for the report and conversation; a model can still truncate input or answer incorrectly.

Local HTTP requests allow up to 2,048 output tokens. When the runtime reports that it stopped at its output limit, AIvia shows an explicit truncation error instead of presenting the partial answer as complete. Ask for a shorter answer; there is no automatic retry. Other reported incomplete or tool-request endings are also rejected. Compatible servers that omit completion metadata remain supported, so undisclosed truncation cannot be detected.

The assistant explains findings and manual next steps only. It has no application action API and cannot change the deterministic score. Local HTTP mode exposes no tools. CLI mode has the additional runtime trust boundary described below.


## Claude Code (installed CLI)

Select **Claude Code · installed CLI**. Use **Detect installed CLIs** to check PATH, or supply the absolute path of an official installed executable. Detection invokes only version/help, without login or inference. **Save & check CLI compatibility** checks the selected executable. It does not verify authentication or enumerate account-entitled models; `default` delegates model selection to the CLI. An explicit model alias is optional.

Review the CLI trust/cloud checkbox and save. In Assistant, send a direct question, or choose report mode and preview the selected report before sending. The official CLI owns authentication and quota. This app never reads or copies its auth files. The CLI environment retains basic runtime and proxy variables, but omits API keys, provider overrides, shell/Node injection variables, and custom configuration-home overrides. Sign in to the official CLI separately before using this connector.

Supported execution platforms: macOS and Linux. Required capabilities are detected from help before any report is sent. The connector uses an empty temporary working directory, stdin for report content, bounded output, no persisted CLI session, disabled built-in model tools, strict empty MCP configuration, disabled user/project hooks and customizations, and CLI safe/restricted modes. Cancellation stops the process group. Raw CLI diagnostics are withheld to avoid leaking credentials or context.

**Administrator policies and hooks can still apply.** The connector does not bypass managed policies and cannot audit arbitrary executables. Trust the installed CLI and its administrator configuration before enabling direct chat. This mode does not guarantee local inference. Windows and incompatible CLI versions can use **Copy reviewed prompt** for manual handoff.

See [Claude programmatic usage](https://code.claude.com/docs/en/headless), [CLI flags](https://code.claude.com/docs/en/cli-reference), and [managed hook behavior](https://code.claude.com/docs/en/hooks#disable-or-remove-hooks). Real authenticated inference is separate from the fixture-based connector tests.

## Codex (manual handoff)

Select **Codex · manual handoff**, save, and preview the direct-chat context or a selected report. Enter an optional question and choose **Copy reviewed prompt**. Paste it into your Codex client yourself. This app sends no report to Codex, reuses no existing Codex conversation, and invokes no Codex command with report content. The receiving client/provider's data policies apply when you submit it there.

Automatic Codex chat is intentionally unavailable in this version: the adapter has not established a documented configuration that disables all model tools. The official `codex exec` read-only sandbox alone still permits reads, so it does not meet the selected-report-only boundary. Detection can report an installed version independently of execution support. See [OpenAI non-interactive documentation](https://learn.chatgpt.com/docs/non-interactive-mode).
