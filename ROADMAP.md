# Baryo Roadmap

Baryo's three pillars: **Software Development**, **DevOps**, and **Research**. Every feature should strengthen at least one of these.

## Philosophy: Local-First with Cloud Escape Hatch

Baryo is a **local-first** tool — models run on your machine via Docker, your data never leaves your laptop, and there are no per-token fees. This is the default and recommended experience.

However, local models have real tradeoffs. A 7B parameter model running in 8GB of RAM cannot match the reasoning depth of a cloud model with 128K+ context and hundreds of billions of parameters. In practice this means:

- **Local models** produce shallower reports, struggle with many tools, and need aggressive prompt/tool filtering to fit within small context windows.
- **Cloud models** (Gemini, OpenRouter) chain complex multi-round tool calls, write deeper analysis, and handle the full MCP tool suite without filtering.

Baryo handles this dynamically — the CLI detects the model's context window and available RAM, then adjusts how many tools and how much prompt context to send. Local models get a compact, filtered set; cloud models get everything. Both use the same pipeline, and the user can switch freely.

The tradeoff is intentional: **privacy and cost vs. capability**. Most daily tasks (code review, quick edits, file exploration) work great locally. For deep research, multi-source reports, and complex agent workflows, cloud models shine. Baryo supports both without compromise.

---

## High Impact — Major Features

### GitHub Workflow (PR, Issues, Code Review)
End-to-end GitHub workflow via the `gh` CLI.

- `/pr` — create a PR from current branch with AI-generated title and description
- `/pr review` — review an open PR (fetch diff, analyze, comment)
- `/issue <number>` — read a GitHub issue, create a branch, start implementing
- `/pr status` — show PR review status (approved/pending/changes requested)
- Respond to PR review comments and push fixes
- Branch management: create, switch, merge, delete

### Multi-Source Search
Strengthen research by querying multiple sources simultaneously.

- Parallel search across multiple providers (DDG + Brave + Tavily)
- Deduplicate and rank results across providers
- `/fetch <url>` improvements: better content extraction, PDF support, structured data
- Domain-specific search: `--site:github.com`, `--site:stackoverflow.com`
- Search result caching to avoid re-fetching within a session
- Configurable number of pages to deep-read (currently 3, allow up to 10)

### Auto-Fix on Lint/Test
Automatically run linters and tests after code changes, fix issues. Inspired by Aider.

- After tool-based code changes, run configured linter (e.g., `golangci-lint`, `eslint`)
- If issues found, feed them back to the model and auto-fix
- Also run tests: `go test`, `npm test`, `pytest` — detected from project type
- Configurable per-project via BARYO.md or config
- Can be combined with hooks system

### Hooks System
Shell commands that run on events — pre-tool, post-tool, on-error, on-commit.

- Define hooks in `~/.baryo/config.yaml` or `.baryo/config.yaml`
- Events: `pre-tool`, `post-tool`, `on-error`, `on-commit`, `on-stream-end`, `on-search`
- Use cases: auto-lint after code changes, format files, run tests, notify
- Hook output shown in chat as tool results
- Blocking hooks can cancel operations (e.g., pre-commit validation)

### Subagent / Task Delegation
Spawn specialized sub-tasks for parallel or isolated work. Inspired by Kimi CLI and Claude Code.

- `/task "description"` — delegate a task to a sub-model call
- Subagent runs in isolated context with its own message history
- Parallel task execution for independent work
- Results merged back into main conversation
- Use cases: research one topic while coding another, run tests in background

### RAG (Retrieval-Augmented Generation)
Index project files and auto-retrieve relevant context.

- Lightweight file-tree + function/type signature index built on startup
- AST-aware repo map (like Aider's tree-sitter approach) for structural understanding
- Auto-retrieve relevant files based on user query without sending entire codebase
- Fall back to existing `glob`/`grep`/`read_file` tools for on-demand access

---

## Additive — Polish & Quality of Life

### DevOps Toolkit
Purpose-built tools for infrastructure, deployment, and container management.

- `/deploy` — generate deployment files (Dockerfile, docker-compose, GitHub Actions, K8s, Terraform)
- `/docker` — manage local containers (list, build, run, stop, logs, exec)
- Docker Compose awareness and CI/CD pipeline generation by project type

### Project Scaffolding
Generate new projects from templates.

- `/new <type>` — scaffold a new project (go-api, react-app, python-cli, etc.)
- Generate boilerplate: main file, config, Dockerfile, CI/CD, README, .gitignore
- Customizable templates stored in `~/.baryo/templates/` or `.baryo/templates/`
- Model fills in project-specific details (name, description, dependencies)

### Shell Toggle (Ctrl-X)
Switch between AI chat and direct shell execution without leaving Baryo. Inspired by Kimi CLI.

- `Ctrl-X` toggles between chat mode and shell mode
- Shell mode: type commands directly, output shown inline
- Prefix with `!` for quick one-off commands (already possible via `/run`)
- Shell history shared with input history

### Model Switching Mid-Session
Switch between models during a conversation without losing context.

- `/model <name>` command to switch mid-session
- Conversation history preserved across model switches
- Use a fast model for simple questions, a powerful one for complex reasoning

### Context Pinning
Always include specific files as context.

- `/pin @file` — pin a file so it's always included in context
- `/unpin @file` — remove a pinned file
- `/pins` — list currently pinned files
- Pinned content injected into every model call alongside system prompt

### Checkpoints & Rewind
Named save-points mid-conversation. Inspired by Claude Code.

- `/checkpoint <name>` — save current conversation + git state
- `/rewind` — roll back to a previous point
- Integrates with git: can restore code state alongside conversation
- Like save-points in a game — explore freely, restore if it goes wrong

### Notification on Completion
Terminal bell or OS notification when a long task finishes.

- Terminal bell on stream completion
- Optional OS notification via `osascript` (macOS) / `notify-send` (Linux)
- Configurable: `notifications: true` in config

### Streaming Speed Metrics
Show tokens/second during streaming.

- Display in status bar alongside token count
- Useful for comparing model performance on local hardware

### Multi-Modal Input
Support image attachments for vision-capable models (LLaVA, etc.).

- `@image path/to/screenshot.png` syntax
- Model capability detection — only enable for vision-capable models
- Useful for UI work, diagram analysis, and visual debugging

### Shell Completions
Tab completion scripts for shell environments.

- `baryo completion zsh/bash/fish/powershell` subcommand
- Complete flags, model names, session IDs

### Worktree Isolation
Agent works on a git worktree so it can't break your main branch.

- Auto-create git worktree for agent code changes
- Changes only merge back on user approval
- `--worktree` flag to enable

### Background Agents
Delegate tasks to background workers.

- Prefix with `&` or `/bg <prompt>` to run in background
- `/tasks` to list running background agents
- Results available when done, don't block main conversation
- Useful for long-running research or code generation

### Sandboxed Code Execution
Run AI-generated code in isolated containers.

- Use Docker (already a dependency) for sandboxing `run_code` and `run_script`
- Prevent accidental damage to the host filesystem
- Optional — users can opt into direct execution for trusted workflows
- `--sandbox` flag to enable

### Session Management Improvements
Better organization and discovery of saved sessions.

- Auto-generated session titles (not just hex IDs)
- `/sessions --search <query>` to search past sessions by content
- Session tagging/labeling
- Auto-cleanup of old sessions (configurable retention)

### Extended Thinking Display
Show/hide model reasoning in a collapsible block.

- `<think>` blocks already parsed — render them as collapsible sections
- Toggle visibility with `/thinking` command
- Dimmed or indented display to distinguish from actual response

### Plugin System
Allow users to add custom tools via config.

- Tool definitions in YAML/JSON config files
- Specify name, description, parameters, and shell command to execute
- Loaded dynamically alongside built-in tools
- Project-level plugins in `.baryo/plugins/` and global in `~/.baryo/plugins/`

---

## Completed

### Agent Modes
- 6 modes: chat (dynamic tools), ask (no tools), code (all tools), architect (read-only), review (read-only), research (read-only)
- `/mode` command to list and switch modes
- Color-coded mode label in status bar (cyan, yellow, purple, orange, green)
- Mode-aware command gating: destructive commands blocked in restricted modes
- System message injection on mode switch for immediate model behavior change
- Mode tags on user messages in history for visual context
- Mode-specific system prompts loaded from embedded prompt files

### Dynamic Model-Aware Agent Pipeline
- Context window detection per model family (8K for Qwen/Phi/Gemma, 32K for Llama/Mistral, 128K+ for Gemini)
- Cloud vs local endpoint detection — cloud models skip all tool filtering, local models get aggressive compaction
- MCP tool filtering: redundant servers (filesystem, git, memory) excluded for small models, all included for large
- Schema trimming and description compaction for small context windows
- Auto-continue on truncation: when `finish_reason == "length"`, automatically sends "Continue" and appends response
- Multi-round tool calling: tools available on every round (not just first), enabling complex chained workflows
- Parallel tool call fix for Gemini (index deduplication for providers that reuse index 0)
- Hallucinated tool call stripping: catches `<tool_call>` and `<tool_code>` (Gemini) blocks
- Ambiguous skill keyword filtering: common words like "report", "memo", "letter" no longer trigger document skills

### Plan Mode
- `/plan <prompt>` enters read-only analysis mode (model can read but not write)
- Model explores codebase and proposes step-by-step implementation plan
- `/plan done` exits plan mode and restores normal tool access
- Header bar shows "plan" indicator when active
- Auto-resets on `/clear`

### MCP (Model Context Protocol) Support
- Connect external tool servers (GitHub, databases, Slack, etc.) via MCP standard
- Server config in `~/.baryo/config.yaml` or `.baryo/config.yaml`
- `/mcp` lists connected servers and their available tools
- MCP tools appear alongside built-in tools transparently
- Works in both interactive and headless (`-p`) modes
- Failed server connections are non-fatal (app continues without them)

### File Write & Edit Tools
- `write_file` tool — create or overwrite files with auto-directory creation
- `edit_file` tool — exact string replacement in existing files
- `delete_file` tool — remove files with permission gating
- Multi-file editing in a single turn
- Permission gating via confirm/suggest/auto modes
- `.baryoignore` and `.gitignore` respected for all write operations

### Permission System
- Three modes: `suggest` (read-only), `confirm` (ask before writes/runs), `auto` (full autonomy)
- Default to `confirm` for destructive operations (file writes, shell commands, `run_code`)
- `--yolo` / `-y` flag sets `auto` mode for unattended operation
- Config via `~/.baryo/config.yaml` (`permission_mode`) or `BARYO_PERMISSION_MODE` env var
- TUI confirmation flow with y/n prompt for destructive tools in confirm mode

### Ignore Files
- `.baryoignore` file (`.gitignore`-style patterns) for project-specific exclusions
- Builtin patterns auto-exclude `.env`, `.env.*`, `*.pem`, `*.key`
- Respected by all tools: `read_file`, `write_file`, `edit_file`, `delete_file`, `glob`, `grep`, `list_directory`, `@mentions`
- Falls back to `git check-ignore` when `.baryoignore` doesn't match
- Batch gitignore checking for @mention completions (single subprocess)

### Deep Research Mode
- `/research <topic>` — multi-round deep research with structured reports
- Configurable depth: quick (1 round), standard (3 rounds), deep (5 rounds)
- Search → fetch top pages → identify gaps → search again → compile report
- Structured output: executive summary, key findings, analysis, numbered source citations
- Context-aware scaling to fit model's context window
- Follow-up in conversation ("dig deeper into finding #3")

### Headless / CI Mode
- Full tool calling in print mode (`-p`) with multi-turn support
- `--yolo` flag for auto-approving all destructive operations
- `--max-turns N` to limit tool-call rounds
- Output formats: `text` (streaming) and `json` (structured)
- Exit codes: 0 (success), 1 (runtime error), 2 (config error)
- Headless executor blocks destructive tools without `--yolo`
- `--no-tools` flag for simple Q&A without tool overhead
- Memories injected into system prompt for headless mode
- Pipe support: `cat file.go | baryo -p "review this" --yolo`

### Cloud Provider Support & Cost Tracking (v0.2.3)
- Gemini and OpenRouter as cloud model providers (Gemini, OpenRouter)
- API key config via YAML (`gemini_api_key`, `openrouter_api_key`) or env vars
- Cloud models appear in model picker and browser with `[gemini]`/`[openrouter]` tags
- Per-session API cost tracking from actual token usage stats
- Cost displayed in status bar for cloud models (e.g. `$0.0012`)
- `/cost` command for session spend breakdown
- Gemini hardcoded pricing table (2.5-pro, 2.5-flash, 2.0-flash)
- OpenRouter pricing parsed from `/models` API response
- `Endpoint` abstraction — local socket, TCP, and HTTPS providers unified
- Print mode (`-p`) works with cloud providers
- Docker health checks skipped for cloud-only usage
- Model selector scrolling for long provider model lists

### Small Model Optimization (v0.2.2)
- XML-structured system prompts with sandwich pattern for better instruction following
- Model family detection (Qwen, Llama, Mistral, Phi, Gemma) with optimized parameter presets
- Qwen3 `/no_think` auto-injection for tool tasks to save tokens
- Dynamic tool gating — tool-call examples only injected when tools are active
- Post-processing guardrails to strip hallucinated `<tool_call>` blocks
- Long conversation reminder injection (>10 messages) to counter "lost in the middle" effect
- Prominent memory injection — user preferences placed right after rules for small model visibility
- Memories injected directly into search summarization prompt for reliable style compliance
- Auto-search on "I don't know" — model automatically triggers web search instead of just suggesting `/search`
- Reduced system prompt token count (~30% fewer tokens in skills.md)
- TopK and Stop token support in ChatParams/ChatRequest

### Skills Integration (v0.2.1)
- 16 Anthropic Agent Skills ported (pdf, docx, pptx, xlsx, slack-gif-creator, frontend-design, etc.)
- Auto-activation by trigger keyword matching
- `/skills` and `/skill <name>` commands
- `run_code` and `run_script` tool execution
- Lazy-loading for fast startup
- Custom skill creation support

### Tool Calling (v0.2.0)
- Built-in tools: `read_file`, `write_file`, `edit_file`, `delete_file`, `glob`, `grep`, `list_directory`, `git_status`, `git_diff`, `git_log`, `gh`, `shell`
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
