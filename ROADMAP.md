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

## Competitive Gap Analysis

This section identifies the critical gaps between Baryo and the leading AI coding CLIs — **Claude Code**, **Aider**, and **OpenCode** — and prioritizes what to build to close (and exceed) them.

### Where Baryo Already Leads
- **Local-first architecture** — no mandatory API key, runs on Docker/Ollama out of the box
- **16+ provider support** — broadest cloud provider coverage (Gemini, OpenRouter, Anthropic, OpenAI, Bedrock, Groq, Mistral, DeepSeek, xAI, Cerebras, Perplexity, SambaNova, Cohere, HuggingFace, GitHub Models, Ollama Cloud)
- **Deep research mode** — multi-round web research with structured reports
- **Skills system** — 16 ported skills (pdf, docx, pptx, xlsx, etc.) with auto-activation
- **SSH tunneling** — seamless remote Ollama access
- **DevOps focus** — a differentiating pillar that competitors largely ignore
- **Dynamic model-aware pipeline** — auto-adjusts tools/context for small vs large models

### Critical Gaps to Close

| Capability | Claude Code | Aider | OpenCode | Baryo |
|---|---|---|---|---|
| Test suite | Extensive | Extensive | Moderate | **Core packages covered** |
| Repo map / tree-sitter | Full AST | Full (with tags) | Partial | 8 languages (Go, TS, JS, Python, Rust, Java, C, C++) |
| Diff/patch strategies | search/replace | unified diff, whole file, udiff | diff-based | exact + fuzzy match, unified diff, whole-file rewrite |
| Extended thinking | Native | N/A | N/A | Native Anthropic + `<think>` block rendering |
| Conversation branching | No | No | No | Checkpoints only |
| Cost budget/limits | Per-session tracking | Per-session tracking | Basic | Tracking only, no limits |
| Voice input | No | Yes | No | No |
| LSP integration | No | No | Yes | No |
| Undo granularity | Per-tool git revert | Git-based undo | Per-change | `/undo` last commit only |

---

## ~~P0 — Foundation & Trust (v0.12)~~ DONE

All P0 items completed. See the Completed section below for details.

---

## Stability Status (v0.12.1)

v0.12.1 was a dedicated stabilization pass — no new features, only hardening. Full details in [CHANGELOG.md](CHANGELOG.md).

### What is solid now

- **Crash safety** — the known panic paths in `apply_diff` are fixed with regression tests; no `panic()` calls remain in production code.
- **Data safety** — all file mutations (edit/write/apply_diff, session saves, memory saves) are atomic; an interrupted write can no longer corrupt a file or lose conversation history.
- **Memory safety** — subprocess output and search/fetch responses are size-capped before buffering.
- **Tool loop correctness** — duplicate-call detection compares full arguments, so multi-file tasks are no longer sabotaged; confirm-mode no longer produces phantom "empty response" errors.
- **Failure visibility** — truncated model streams and dead MCP servers surface real errors immediately instead of silent truncation or repeated 30s timeouts.
- **Sandboxing** — destructive file tools resolve symlinks before the project-root containment check.
- **Supply chain** — `govulncheck` clean (dependencies patched, `toolchain go1.25.12` pinned); `staticcheck` clean; CI runs vet, gofmt, staticcheck, govulncheck, tests with and without CGO, and the race detector on Linux and macOS.

### Known gaps still open

| Gap | Impact | Notes |
|---|---|---|
| No tests for `internal/tui` (~10k lines) | Regressions in the UI state machine are only caught manually | Largest remaining test gap; the confirm-flow bug fixed in v0.12.1 is exactly the class of bug tests here would catch |
| Unbounded session growth | Long conversations slow saves and bloat disk | Sessions are fully rewritten every turn; cleanup is age-based only (`session_retention_days`) |
| No MCP auto-reconnect | A crashed MCP server stays down for the session | Failures are now reported immediately, but recovery requires restarting baryo |
| Process-group kill is unix-only | On Windows, shell grandchildren can outlive the tool timeout | `procutil.SetProcessGroup` is a no-op on Windows |
| `worktree`/`session` packages untested | Lower risk (small, simple code) | Candidates for quick test coverage |

---

## P1 — Competitive Parity (v0.13)

Features that match the best-in-class competitors and remove reasons for users to switch away.

### Agentic Tool Loop Improvements
**Gap:** Claude Code runs up to 200+ tool rounds in a single turn. Baryo caps at 5 (`maxToolRounds`). Aider supports unlimited rounds.

- Raise `maxToolRounds` to 25 (configurable via `max_tool_rounds` in config)
- Add `--max-turns` to interactive mode (currently only in headless)
- **Parallel tool execution** — when the model requests multiple independent tool calls in one turn, execute them concurrently (currently sequential)
- Add a **tool call cost estimator** — show estimated cost before executing expensive tool chains in `confirm` mode
- Better tool result truncation — smart summarization instead of hard character cutoff

### Cost Budget & Spend Limits
**Gap:** Baryo tracks cost but has no guardrails. Claude Code and Aider let users set spending limits.

- `cost_limit` config option (per-session dollar cap)
- Warning at 80% of budget, hard stop at 100%
- `/cost` already exists — extend with per-tool and per-round breakdown
- Cumulative daily/weekly spend tracking across sessions

### Conversation Branching
**Gap:** No competitor has this well, but Baryo's checkpoint system is close. This would be a differentiator.

- Extend `/checkpoint` + `/rewind` into full conversation branching
- `/branch` within a conversation — fork the conversation at any point
- Visual branch tree in `/sessions` view
- Compare branches: show what each branch produced

### Git Integration Depth
**Gap:** Aider has the deepest git integration — auto-commits every change with descriptive messages, groups related changes, and supports `--auto-commit`. Claude Code auto-commits on request.

- **Auto-commit mode** — optionally commit each successful edit with an AI-generated message (`auto_commit: true` in config)
- **Semantic commit grouping** — batch related file changes into a single commit
- Smarter `/undo` — undo individual tool actions, not just the last commit
- `/stash` and `/stash pop` — stash current changes mid-conversation
- Show git blame context for edited files so the model understands change history

### LSP Integration
**Gap:** OpenCode integrates with LSP for diagnostics, go-to-definition, and symbol lookup. This gives the model precise compiler-level feedback.

- Connect to running LSP servers (gopls, typescript-language-server, pyright, rust-analyzer)
- Feed LSP diagnostics (errors, warnings) into tool results after edits — more precise than running the linter
- LSP-powered symbol lookup: resolve function signatures, type definitions, references
- Auto-detect LSP servers by project type

---

## P2 — Differentiation (v0.14)

Features that make Baryo the clear choice over competitors, especially for its target audience (local-first, DevOps-aware, multi-provider).

### Multi-Source Search (already planned)
Strengthen research by querying multiple sources simultaneously.

- Parallel search across multiple providers (DDG + Brave + Tavily)
- Deduplicate and rank results across providers
- `/fetch <url>` improvements: better content extraction, PDF support, structured data
- Domain-specific search: `--site:github.com`, `--site:stackoverflow.com`
- Search result caching to avoid re-fetching within a session
- Configurable number of pages to deep-read (currently 3, allow up to 10)

### Auto-Mode with Model Routing
**Gap:** No competitor automatically routes tasks to the best model. Baryo has `auto_mode` config but it's basic tier-based routing.

- **Intent classification** — analyze the user's prompt and route to the best model (fast model for simple questions, strong model for complex code changes)
- Cost-aware routing: prefer cheaper models when the task doesn't need reasoning depth
- Automatic fallback: if a local model fails a tool call, retry on a cloud model
- Per-task routing: research tasks → Perplexity/Gemini, code tasks → Claude/GPT, quick edits → local model

### Hooks System (already planned)
Shell commands that run on events — pre-tool, post-tool, on-error, on-commit.

- Define hooks in `~/.baryo/config.yaml` or `.baryo/config.yaml`
- Events: `pre-tool`, `post-tool`, `on-error`, `on-commit`, `on-stream-end`, `on-search`
- Use cases: auto-lint after code changes, format files, run tests, notify
- Hook output shown in chat as tool results
- Blocking hooks can cancel operations (e.g., pre-commit validation)

### Subagent / Task Delegation (already planned)
Spawn specialized sub-tasks for parallel or isolated work.

- `/task "description"` — delegate a task to a sub-model call
- Subagent runs in isolated context with its own message history
- Parallel task execution for independent work
- Results merged back into main conversation
- Use cases: research one topic while coding another, run tests in background

### Plugin System (already planned, expand scope)
Allow users to add custom tools via config.

- Tool definitions in YAML/JSON config files
- Specify name, description, parameters, and shell command to execute
- Loaded dynamically alongside built-in tools
- Project-level plugins in `.baryo/plugins/` and global in `~/.baryo/plugins/`
- **Community plugin registry** — curated list of community plugins on the website/repo
- **Plugin hooks** — plugins can register for lifecycle events

### DevOps Toolkit (already planned)
Purpose-built tools for infrastructure, deployment, and container management.

- `/deploy` — generate deployment files (Dockerfile, docker-compose, GitHub Actions, K8s, Terraform)
- `/docker` — manage local containers (list, build, run, stop, logs, exec)
- Docker Compose awareness and CI/CD pipeline generation by project type
- **Infrastructure-as-Code review** — analyze Terraform/CloudFormation/Pulumi files for best practices
- **Log analysis** — pipe container/service logs into Baryo for diagnosis

---

## P3 — Polish & Quality of Life (v0.15+)

### Session Management Improvements (already planned)
- Auto-generated session titles (not just hex IDs)
- `/sessions --search <query>` to search past sessions by content
- Session tagging/labeling
- Auto-cleanup of old sessions (configurable retention)

### Voice Input
**Gap:** Aider supports voice input via the microphone. Unique differentiator for accessibility.

- `/voice` command to start recording
- Local transcription via Whisper (keeps the local-first promise)
- Cloud transcription fallback for accuracy
- Voice-to-command: natural language → slash command mapping

### Multi-File Awareness
- When the model edits file A, automatically show related files (imports, tests, interfaces) as context
- Dependency graph awareness: editing a function should surface all callers
- Test file association: editing `foo.go` should surface `foo_test.go`

### TUI Improvements
- Split-pane view: code on one side, conversation on the other
- File tree sidebar (toggleable)
- Inline code preview for `@mentions` before sending
- Better progress indicators for long-running tool chains (progress bar, ETA)
- Keyboard shortcuts reference overlay (`?` key)

### Streaming Optimizations
- **Speculative decoding awareness** — detect and leverage speculative decoding on supported providers
- Streaming diff display — show edits being applied in real-time
- Interruptible tool execution — `Ctrl-C` cancels the current tool without killing the conversation

### Documentation & Onboarding
- `baryo tutorial` — interactive walkthrough of key features
- In-app help: `/help <topic>` with contextual examples
- Video demos linked from README
- Contributing guide for plugin authors

---

## Completed

### Foundation & Trust (v0.12.0)

**Test Suite + CI Pipeline:**
- GitHub Actions CI workflow (vet, test, build on ubuntu + macos)
- Makefile with build, test, vet, lint, fmt, coverage targets
- 100+ unit tests across 6 core packages: `internal/llm`, `internal/tools`, `internal/config`, `internal/rag`, `internal/index`, `internal/search`
- Table-driven tests for provider detection, pricing lookup, BM25 ranking, config merging, HTML parsing, tool execution

**Smarter Diff/Edit Strategy:**
- `edit_file` fuzzy whitespace matching — tolerates tab/space and indentation differences
- `edit_file` whole-file rewrite mode for files under 100 lines (empty `old_string`)
- New `apply_diff` tool — unified diff parser with multi-hunk support for bulk edits in one call
- Context line validation to prevent misapplied patches

**Repo Map Language Parsers:**
- Added Rust, Java, C, C++ tree-sitter parsers (8 languages total)
- Rust: functions, structs, enums, traits, impl methods
- Java: classes, interfaces, methods, constructors
- C: functions, structs, enums
- C++: everything from C plus classes with method extraction
- New file extensions: `.rs`, `.java`, `.c`, `.h`, `.cpp`, `.cc`, `.cxx`, `.hpp`

**Extended Thinking Rendering:**
- `show_thinking` config field + `BARYO_SHOW_THINKING` env var
- `/thinking` toggle command in TUI
- `<think>` block parsing returns extracted thinking content
- Native Anthropic extended thinking API support (Claude 3.5 Sonnet, Claude Sonnet 4, Claude Opus 4)
- `ThinkingToken` events streamed in real-time with dimmed/italic rendering
- Thinking content shown above assistant response in history

### GitHub Workflow (v0.11.0)
- `/pr` — create a PR from current branch with AI-generated title and description
- `/pr review [number]` — review a PR (fetch diff + comments, stream analysis)
- `/pr status` — show PR review status (approved/pending/changes requested)
- `/issue <number>` — read a GitHub issue and get implementation suggestions
- `/branch <name>` — create and checkout a feature branch
- Meta-tools: `review_pr`, `read_issue`, `pr_status`, `create_branch`
- Read-only tools work in all modes; `create_branch` gated behind permission system

### Project Scaffolding (v0.10.0)
- `/new <type>` — scaffold a new project (go-api, react-app, python-cli, etc.)
- Generate boilerplate: main file, config, Dockerfile, CI/CD, README, .gitignore
- Customizable templates stored in `~/.baryo/templates/` or `.baryo/templates/`

### Shell Toggle (v0.10.0)
- `Ctrl-X` toggles between chat mode and shell mode
- Shell mode: type commands directly, output shown inline
- Shell history shared with input history

### Model Switching Mid-Session (v0.10.0)
- `/models` command to switch mid-session
- Conversation history preserved across model switches

### Context Pinning (v0.10.0)
- `/pin @file`, `/unpin @file`, `/pins` commands
- Pinned content injected into every model call alongside system prompt

### Checkpoints & Rewind (v0.10.0)
- `/checkpoint <name>` — save current conversation + git state
- `/rewind` — roll back to a previous checkpoint

### Notification on Completion (v0.10.0)
- Terminal bell on stream completion
- OS notification via `osascript` (macOS) / `notify-send` (Linux)

### Streaming Speed Metrics (v0.10.0)
- Tokens/second in status bar alongside token count

### Multi-Modal Input (v0.10.0)
- `@image path/to/screenshot.png` syntax for vision-capable models
- Model capability detection — only enable for vision-capable models

### Shell Completions (v0.10.0)
- `baryo completion zsh/bash/fish/powershell` subcommand

### Worktree Isolation (v0.10.0)
- `--worktree` flag for isolated agent code changes
- Changes only merge back on user approval

### Background Agents (v0.10.0)
- `/bg <prompt>` to run in background, `/tasks` to list
- Results available when done, don't block main conversation

### Sandboxed Code Execution (v0.10.0)
- Docker-based sandboxing for `run_code` and `run_script`
- `--sandbox` flag to enable

### Auto-Fix on Lint/Test (v0.9.0)
- Auto-run linter and/or tests after `edit_file`, `apply_diff`, `write_file`, `delete_file` tool calls
- Errors appended to tool result so model sees and self-corrects immediately
- Auto-detect project type: Go (`golangci-lint`/`go vet`), Node (`eslint`), Rust (`cargo clippy`), Python (`flake8`)
- Auto-detect test runner: `go test`, `jest`, `cargo test`, `pytest`
- Custom command overrides via `lint_command` / `test_command` config
- 30-second timeout per command, output truncated to 4000 chars
- Config: `auto_lint` / `auto_test` (default false), env vars `BARYO_AUTO_LINT` / `BARYO_AUTO_TEST`

### RAG Source File Indexing (v0.9.0)
- Third RAG store: indexes project source files (`.go`, `.ts`, `.py`, `.rs`, `.java`, etc.)
- Symbol-based chunking when tree-sitter index available (one chunk per function/type)
- Line-based chunking fallback (~800 char windows with 3-line overlap)
- Three-way budget split: 40% sources, 30% docs, 30% sessions
- Up to 500 files indexed, code files prioritized over config/docs
- Async two-phase startup: source indexing starts after both RAG and repo index are ready
- Respects `.gitignore` and `.baryoignore` rules

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
- `edit_file` tool — string replacement with fuzzy whitespace matching and whole-file rewrite mode
- `apply_diff` tool — unified diff application with multi-hunk support
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
- Built-in tools: `read_file`, `write_file`, `edit_file`, `apply_diff`, `delete_file`, `glob`, `grep`, `list_directory`, `git_status`, `git_diff`, `git_log`, `gh`, `shell`
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
