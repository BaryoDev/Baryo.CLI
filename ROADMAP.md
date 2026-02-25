# Baryo Roadmap

---

## Core — Essential Next Steps

Features that fill critical gaps and make Baryo competitive with Claude Code, Gemini CLI, and Copilot CLI.

### Memory (Persistent Context Across Sessions)
Allow the model to remember things across chat sessions — user preferences, project decisions, learned facts, and recurring instructions.

- `/remember <fact>` — explicitly save something (e.g., "always use TypeScript", "project uses PostgreSQL")
- `/forget <fact>` — remove a saved memory
- `/memories` — list all saved memories
- Auto-detect memorable moments — when the user corrects the model or states a preference, offer to remember it
- Memories stored in `~/.baryo/memories.json` (global) and `.baryo/memories.json` (per-project)
- Injected into system prompt at session start; per-project takes priority over global

### Permission System
Tiered permission levels for tool execution and file modification. Every major CLI tool has this.

- Modes: `suggest` (read-only), `confirm` (ask before writes/runs), `auto` (full autonomy, like Gemini's yolo mode)
- Default to `confirm` for destructive operations (file writes, shell commands, `run_code`)
- Per-tool allow/deny overrides (e.g., always auto-approve `read_file`, always confirm `run_code`)
- Config via `~/.baryo/config.yaml` or `--mode` flag

### Model Switching Mid-Session
Switch between models during a conversation without losing context. Basic expectation for any multi-model tool.

- `/model <name>` command to switch mid-session
- Conversation history preserved across model switches
- Use a fast model for simple questions, a powerful one for complex reasoning

### Ignore Files
Prevent the model from reading sensitive files. Security baseline.

- `.baryoignore` file (similar to `.gitignore` patterns)
- Auto-exclude `.env`, credentials, secrets, API keys
- Respected by all tools (`readfile`, `glob`, `grep`, `@mentions`)
- Falls back to `.gitignore` rules when `.baryoignore` doesn't exist

### Context Pinning
Always include specific files as context without re-mentioning them.

- `/pin @file` — pin a file so it's always included in context
- `/unpin @file` — remove a pinned file
- `/pins` — list currently pinned files
- Pinned content injected into every model call alongside system prompt

### Notification on Completion
Terminal bell or OS notification when a long task finishes. Small but essential UX.

- Terminal bell on stream completion
- Optional OS notification via `osascript` (macOS) / `notify-send` (Linux)
- Configurable: `notifications: true` in config

---

## Important — High-Impact Features

Features that significantly expand what Baryo can do. Larger effort but strong differentiators.

### Hooks System
Shell commands that run on events — pre-tool, post-tool, on-error. Inspired by Claude Code.

- Define hooks in `~/.baryo/config.yaml` or `.baryo/config.yaml`
- Events: `pre-tool`, `post-tool`, `on-error`, `on-commit`, `on-stream-end`
- Use cases: auto-lint after code changes, format files, run tests, send Slack notifications
- Hook output shown in chat as tool results

### Agent Modes
Specialized modes with different tool access and behavior. Inspired by Copilot CLI and Aider.

- `/mode ask` — read-only answers, no tools, fast (extends existing `/ask`)
- `/mode code` — model can read/write files, run tools, execute code
- `/mode architect` — high-level planning, generates step-by-step plans without executing
- `/mode review` — focused on code review with security and style checks
- Mode persists for the session, switchable at any time

### PR Workflow
End-to-end pull request workflow. Inspired by Copilot CLI.

- `/pr` — create a PR from current branch with AI-generated title and description
- `/pr review` — review an open PR (fetch diff, analyze, comment)
- Issue-to-code: `/issue <number>` — read a GitHub issue, create a branch, implement it
- Respond to PR review comments and update code

### Auto-Fix on Lint
Automatically run linters after changes and fix detected issues. Inspired by Aider.

- After tool-based code changes, run configured linter (e.g., `golangci-lint`, `eslint`)
- If issues found, feed them back to the model and auto-fix
- Configurable per-project via BARYO.md or config
- Can be combined with hooks system

### RAG (Retrieval-Augmented Generation)
Index project files and retrieve relevant context automatically instead of requiring manual `@mentions`.

- Lightweight file-tree + function/type signature index built on startup or on-demand
- AST-aware repo map (like Aider's tree-sitter approach) for structural understanding
- Auto-retrieve relevant files based on user query without sending the entire codebase
- Fall back to existing `glob`/`grep`/`readfile` tools for on-demand access

### Multi-Modal Input
Support image attachments in chat for models that support vision (LLaVA, etc.).

- `@image path/to/screenshot.png` syntax or paste from clipboard
- Model capability detection — only enable for vision-capable models
- Useful for UI work, diagram analysis, and visual debugging

### MCP (Model Context Protocol) Support
Integrate with the MCP standard for universal tool connectivity. Adopted by Anthropic, OpenAI, Google, Microsoft.

- Connect to external MCP servers (Figma, Jira, GitHub, Slack, databases, etc.)
- MCP server config in `~/.baryo/config.yaml` or `.baryo/config.yaml`
- Dynamically register tools from MCP servers alongside built-in tools
- Thousands of existing MCP servers available immediately

### Sandboxed Code Execution
Run AI-generated code in isolated containers. Inspired by Codex CLI and Gemini CLI.

- Use Docker (already a dependency) for sandboxing `run_code` and `run_script`
- Prevent accidental damage to the host filesystem
- Optional — users can opt into direct execution for trusted workflows
- Configurable: `sandbox: true` in config or `--sandbox` flag

---

## Additive — Nice-to-Haves

Features that polish the experience. Lower priority but each one makes Baryo feel more complete.

### Shell Completions
Tab completion scripts for shell environments.

- `baryo completion zsh/bash/fish/powershell` subcommand
- Complete flags, model names, session IDs

### Themes
Built-in color schemes. Inspired by Gemini CLI.

- 2-3 built-in themes (dark, light, minimal)
- Auto-detect terminal background color
- Custom theme support via config
- `/theme <name>` command to switch

### Streaming Speed Metrics
Show tokens/second during streaming.

- Display in status bar alongside token count
- Useful for comparing model performance on local hardware

### Session Management Improvements
Better organization and discovery of saved sessions.

- Auto-generated or user-set session titles (not just hex IDs)
- `/sessions --search <query>` to search past sessions by content
- Session tagging/labeling for grouping (e.g., `tag:debugging`, `tag:feature-x`)
- Auto-cleanup of old sessions (configurable retention)

### Checkpoints
Named save-points mid-conversation. Inspired by Gemini CLI.

- `/checkpoint <name>` — save the current conversation state
- `/restore <name>` — roll back to a named checkpoint
- Like save-points in a game — explore freely, restore if it goes wrong

### Conversation Branching
Fork a conversation at any point to explore different directions without losing the original thread.

- `/branch` to create a fork from the current point
- `/branches` to list and switch between branches
- Session format would need tree structure support

### Worktree Isolation
Agent works on a git worktree so it can't break your main branch. Inspired by Claude Code.

- Auto-create git worktree for agent code changes
- Changes only merge back to main branch on user approval
- Prevents accidental damage to working directory

### Plugin System
Allow users to add custom tools via a plugin config.

- Tool definitions in YAML/JSON config files
- Specify name, description, parameters, and shell command to execute
- Loaded dynamically alongside built-in tools
- Project-level plugins in `.baryo/plugins/` and global in `~/.baryo/plugins/`

### Background Agents
Delegate tasks to background workers. Inspired by Claude Code.

- Prefix with `&` or `/bg <prompt>` to run in background
- `/tasks` to list running background agents
- `/resume <id>` to check on or continue a background task
- Useful for long-running code generation or research

### Extended Thinking Display
Show/hide model reasoning in a collapsible block. Inspired by Claude Code.

- `<think>` blocks already parsed — render them as collapsible sections
- Toggle visibility with `/thinking` command
- Dimmed or indented display to distinguish from actual response

### Cost Tracking
Track token usage per session. Inspired by Claude Code.

- Running total of tokens used (input + output) per session
- Estimated cost for API-based providers (if configured)
- `/usage` command to show session stats

---

## Completed

### Skills Integration (v0.2.1)
- 16 Anthropic Agent Skills ported (pdf, docx, pptx, xlsx, slack-gif-creator, frontend-design, etc.)
- Auto-activation by trigger keyword matching
- `/skills` and `/skill <name>` commands
- `run_code` and `run_script` tool execution
- Lazy-loading for fast startup
- Custom skill creation support

### Tool Calling (v0.2.0)
- Built-in tools: `read_file`, `glob`, `grep`, `list_directory`, `git_status`, `git_diff`, `git_log`, `gh`
- Native OpenAI tool-calling API + text-based fallback parser
- Git workflow commands: `/diff`, `/commit`, `/review`, `/undo`
- `/run` for shell commands, `/ask` for tool-free answers

### Deep Web Search (v0.2.0)
- `/search` auto-fetches top result pages and summarizes with source citations
- Model suggests searching instead of hallucinating
- Auto-triggers search on user agreement ("yes", "sure", "yes search for it")
- Smart fallback when model fails to search via tool calling
- Context compaction after summary to save tokens
- Three providers: DuckDuckGo (default), Brave, Tavily

### @ Mentions (v0.2.0)
- `@filepath` with live tab completion and recursive search
- File contents injected as context
- Gitignore-aware, binary filtering, 100KB limit

### SSH Tunnel (v0.2.0)
- Auto-launch SSH tunnels to remote Ollama servers
- `--tunnel user@host` flag and YAML config
- Auto-teardown on exit

### Project Instructions (v0.2.0)
- `BARYO.md` for per-project model customization (project, config dir, global)
- `/init` auto-generates project instructions

### Context Management (v0.2.0)
- Token usage tracking with color-coded status bar
- Auto-compaction at 85% capacity
- `/compact` and `/context` commands

### Session Persistence (v0.1.0)
- Auto-save after each turn
- Resume with `-c`, `-r`, `--resume-id`
- `/sessions` browser

### Core Chat (v0.1.0)
- Bubble Tea TUI with streaming, markdown rendering, input history
- Model picker with Docker Model Runner integration
- Print mode for pipelines (`-p`)
- Diagnostic checks (`/doctor`, `baryo doctor`)
- Conversation export (`/export`, `/copy`)
