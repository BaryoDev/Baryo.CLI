# Baryo Roadmap

Baryo's three pillars: **Software Development**, **DevOps**, and **Research**. Every feature should strengthen at least one of these.

Last reconciled against the source tree at v0.13.0. Competitor claims in this document were verified against primary sources (vendor documentation and source repositories) on 2026-09-09; re-verify before relying on them, as this is the section that goes stale fastest.

## Philosophy: Local-First with Cloud Escape Hatch

Baryo is a **local-first** tool — models run on your machine via Docker, your data never leaves your laptop, and there are no per-token fees. This is the default and recommended experience.

However, local models have real tradeoffs. A 7B parameter model running in 8GB of RAM cannot match the reasoning depth of a cloud model with 128K+ context and hundreds of billions of parameters. In practice this means:

- **Local models** produce shallower reports, struggle with many tools, and need aggressive prompt/tool filtering to fit within small context windows.
- **Cloud models** (Gemini, OpenRouter) chain complex multi-round tool calls, write deeper analysis, and handle the full MCP tool suite without filtering.

Baryo handles this dynamically — the CLI detects the model's context window and available RAM, then adjusts how many tools and how much prompt context to send. Local models get a compact, filtered set; cloud models get everything. Both use the same pipeline, and the user can switch freely.

The tradeoff is intentional: **privacy and cost vs. capability**. Most daily tasks (code review, quick edits, file exploration) work great locally. For deep research, multi-source reports, and complex agent workflows, cloud models shine. Baryo supports both without compromise.

---

## Primary Deployment Target

Competing with Claude Code and Cursor on general-purpose interactive coding is not a winnable frame — they have more engineers on the tool loop alone than this project has contributors, and the parity gaps below are widening rather than closing.

The defensible position is narrower: **the coding agent for people who cannot or will not send their code to a cloud.** An always-on agent, on hardware the user owns, running against code that never leaves it, at zero marginal cost per task.

[DEPLOYMENT.md](DEPLOYMENT.md) specs this out — topologies, hardware guidance, the headless/systemd path, and the gaps that block it today. When prioritizing, prefer work that serves that target over work that closes a parity gap for its own sake.

Concretely, this reframing means:

- **Deprioritized:** voice input, LSP integration, split-pane TUI. These matter for interactive desktop use, which is not where Baryo wins.
- **Prioritized:** unattended reliability (timeouts, resource bounds), the trust model, small-model context efficiency, and anything that reduces per-task cost on constrained hardware.

---

## Competitive Gap Analysis

Gaps between Baryo and the leading AI coding CLIs — **Claude Code**, **Aider**, and **OpenCode**.

### Where Baryo genuinely leads

These survived verification against the competitors as of 2026-09.

- **Local-first architecture** — no mandatory API key, runs against Docker Model Runner or Ollama out of the box. Local endpoints send no `Authorization` header at all, so there is no accidental-egress path.
- **Single static binary** — no Node or Python runtime to install. Aider requires a Python environment; this matters disproportionately on ARM and in locked-down environments.
- **17 cloud providers + Bedrock** in one config (`internal/llm/provider.go:19-37`), switchable mid-session.
- **Dynamic model-aware pipeline** — context-window detection driving tool filtering, schema trimming, and prompt compaction. This is the hard part of making a small local model usable as an agent, and it is the piece of this codebase with the most genuine novelty.
- **SSH tunneling** to a remote model host, for splitting the harness from the GPU.

### Claims retired from this section

Previously listed as leads, but they no longer distinguish Baryo:

- ~~Deep research mode~~ — Claude Code ships `/deep-research`.
- ~~Skills system~~ — Claude Code has skills and a plugin marketplace; the mechanism is no longer unusual.
- ~~DevOps focus~~ — a stated pillar with no DevOps-specific tooling in the codebase. `/deploy` and `/docker` do not exist; only `/new` emits Dockerfiles. Re-earn this claim by shipping the P2 DevOps toolkit, or drop the pillar.

### Capability comparison

Verified 2026-09-09 against vendor documentation and source.

| Capability | Claude Code | Aider | OpenCode | Baryo |
|---|---|---|---|---|
| Conversation branching | `/branch`, `/fork`, `/rewind` (code + conversation) | No | `run --fork`, fork-from-message | **Behind** — checkpoints are in-memory only, lost on exit |
| LSP integration | 11 languages, diagnostics auto-injected after every edit | No | Partial | **Behind** — no LSP; `autofix.go` runs a linter instead |
| Voice input | `/voice` dictation | Yes | No | No (deprioritized, see above) |
| Cost limits | `--max-budget-usd` hard stop (print mode only) | Tracking only, no cap | Basic | **Behind** — tracking only |
| Tool rounds per turn | Effectively unbounded | Unbounded | Unbounded | **Behind** — hard cap of 5 interactive (`internal/llm/toolloop.go:19`); print mode configurable via `--max-turns` |
| Parallel tool execution | Yes | N/A | Yes | **Behind** — sequential (`toolloop.go:236-282`) |
| Repo map / tree-sitter | Full AST + LSP symbols | Full, with ranked tags | Partial | 8 languages — **but dead in every released binary**, see [#9](https://github.com/BaryoDev/Baryo.CLI/issues/9) |
| Diff/patch strategies | search/replace | unified diff, whole file, udiff | diff-based | exact + fuzzy, unified diff, whole-file rewrite |
| Extended thinking | Native | N/A | N/A | Native Anthropic + `<think>` rendering |
| Local-first, no API key | No | Partial | No | **Leads** |
| Single binary, no runtime | No (Node) | No (Python) | No (Node) | **Leads** |
| Undo granularity | Per-tool revert + `/rewind` | Git-based undo | Per-change | `/undo` last commit only |

Two rows previously claimed as Baryo leads were wrong: conversation branching is now table stakes (two of three competitors have it), and the claim that Aider offers spending limits does not hold — Aider has no cost cap of any kind. A Baryo `cost_limit` that works in **interactive** mode would be genuinely ahead, since Claude Code's `--max-budget-usd` is print-mode only.

---

## P0 — Correctness & Trust (v0.14)

Defects found in the v0.13.0 review. These block the deployment target and, in two cases, make the tool unsafe to point at code the user did not write. Nothing in P1 or below should start until these are closed.

### Shipped artifact is broken

- **[#9](https://github.com/BaryoDev/Baryo.CLI/issues/9) — released binaries index zero files.** `CGO_ENABLED=0` in `.goreleaser.yaml` compiles in the no-CGO `ParseFile`, which returns an error for every file; `Index.parseOne` propagates it and both `Build` and `Update` skip the file. Measured: CGO on indexes 175/175 files with a 5780-byte repo map, CGO off indexes 0. Affects every distribution channel on every platform.
- **[#6](https://github.com/BaryoDev/Baryo.CLI/issues/6) — the documented `go install` command cannot work.** `go.mod` declares `github.com/arnelirobles/baryo-cli`; README documents `github.com/baryodev/baryo-cli`; the repository is at `BaryoDev/Baryo.CLI`. No path resolves.

### Trust and containment

- **[#12](https://github.com/BaryoDev/Baryo.CLI/issues/12) — project config and skills are trusted implicitly.** A cloned repository's `.baryo/config.yaml` can spawn MCP servers, register `sh -c` hooks, set `permission_mode: auto`, and redirect `socket_path` — all before the user types anything. A repo-supplied skill triggers `pip install` with no confirmation. Needs a directory-trust gate.
- **Read tools are not contained.** `read_file`, `grep`, `glob`, and `list_directory` prefix-check the *unresolved* path; only the write tools call `resolveWithinProject`. A symlink in a cloned repo reads arbitrary files, and `fetch_page` is ungated, so injection → read → exfiltrate needs no approval in `confirm` mode. `internal/tools/paths_test.go` already tests this property for writes.
- **`gh api` GET-only allowlist is bypassable.** `internal/tools/gh.go:166,190` matches `-X`/`--method`/`-f`/`--field` by exact token, so `--method=DELETE` and `--input body.json` pass. The tool is not marked `Destructive`.
- **MCP tools skip the permission gate** in every executor, including plan mode, which the UI presents as read-only.

### Correctness

- **Anthropic tool calls with no text preamble fail the turn.** `toolloop.go:280` appends an assistant message carrying only text and no tool calls, so `anthropic.go:152-153` emits `{"role":"assistant","content":""}`, which the API rejects. With extended thinking on, thinking goes to `ThinkingToken` and the text buffer is empty — so this should fire routinely. Both converters already support `tool_use`/`tool_result` blocks; the loop never populates them.
- **Duplicate-call suppression blocks legitimate re-runs.** `toolloop.go:253-260` records the key *before* execution and covers every tool, so `go test` → fix → `go test` returns "Already retrieved results for this query". The fix→verify loop cannot close. Restrict to idempotent search-family tools and only record successful calls.
- **`edit_file` fuzzy matching can corrupt an unrelated line.** `editfile.go:153` does `strings.Replace(content, match, ...)`, a substring search that need not land on the window `fuzzyFind` located. Reported as success. Splice by line index instead.
- **No cancellation path.** `app.go:352` returns `tea.Quit` on ctrl+c before dispatching to any screen, making the cancel handler at `chat.go:628-633` dead code. A hung `deep_research` or a long shell tool can only be escaped by quitting, which also orphans tool children.
- **`/models` mid-session discards the conversation.** `transitionToChat` (`app.go:649`) calls `NewChat(...)`, contradicting the documented behavior that history is preserved.
- **Stale flow flags delete the user's next message.** `resetStreamState` clears `compactPending` but not `searchPending`/`researchPending`/`strategyPending`; after a stream error the index arithmetic in `Done` splices out a real user message. Same bug class as the confirm-flow bug fixed in v0.12.1.
- **[#7](https://github.com/BaryoDev/Baryo.CLI/issues/7) — no stream timeout**; headless hangs forever on a stalled provider.
- **One oversized MCP message kills the server for the session.** A JSON-RPC line over 1 MiB makes the scanner return `ErrTooLong`; `readLoop` exits and sets `dead` permanently, with no reconnect.
- **Cost accounting is wrong.** Only the last round's usage is reported (`toolloop.go:178,295`), and the OpenAI-compatible request never sets `stream_options.include_usage`, so OpenAI-family providers report `$0.00` forever.

### Performance

- **[#10](https://github.com/BaryoDev/Baryo.CLI/issues/10) — `git check-ignore` forked once per file.** ~2.1 ms per spawn; a `grep` over 10k files spends ~20 s in process creation alone, per call. Batch it.
- **[#11](https://github.com/BaryoDev/Baryo.CLI/issues/11) — session file rewritten in full every turn.** Write amplification on flash storage in an always-on deployment.

### Test coverage

`internal/tui` is 10,754 lines with zero tests, and is where the last two bugfix releases landed. Bubble Tea models are pure `Update(msg)` functions and are testable without a terminal. Highest-value first: confirm approve/deny listener arming, stream-error flag clearing, concurrent-stream clobbering, `Done` turn assembly, compaction splice arithmetic.

---

## P1 — Agentic Capability (v0.15)

Previously labeled v0.13. None of it shipped in v0.13.0 — that release was the compaction/archive work — so it is renumbered rather than marked late.

The theme is closing the gap between "assistant that edits files" and "agent that finishes tasks". The round cap is the single highest-leverage item in the document.

### Tool loop

- **Raise the interactive round cap.** `maxToolRounds = 5` is the ceiling on everything: it gates the TUI, headless, and subagents alike. Five rounds reads a file and edits it; it does not chase a failing test to root cause. Make it configurable (`max_tool_rounds`, default 25) and add `--max-turns` to interactive mode.
- **Parallel tool execution** — the loop already collects results into a slice, so fanning out independent calls is contained.
- **Smarter tool-result truncation** — summarize rather than hard-cut. Matters most for small context windows.
- **Cap MCP tool results** before they enter the conversation; they are currently appended uncapped.

### Cost budget and spend limits

- `cost_limit` config option with a hard stop, working in **interactive** mode as well as headless — this is where Baryo can be ahead rather than at parity.
- Warning at 80% of budget, stop at 100%.
- Accumulate usage across rounds and set `stream_options.include_usage` (see P0 — the current numbers are wrong, so build the guardrail on a correct meter).
- Extend `/cost` with a per-tool and per-round breakdown; persist spend per session for daily/weekly totals.

### Conversation branching

Table stakes, not a differentiator — both Claude Code and OpenCode ship it.

- Persist checkpoints; they are in-memory today and die with the process.
- `/fork` to branch the conversation at any point. **Do not name it `/branch`** — that already means git branch (`chat.go:2394`).
- Reuse the existing `session.Session` ID/Messages/Archive layer for fork-at-message.

### Git integration depth

- **Auto-commit mode** — optionally commit each successful edit with a generated message (`auto_commit: true`). Aider's deepest advantage.
- Semantic commit grouping — batch related file changes into one commit.
- Smarter `/undo` — undo individual tool actions, not just the last commit.
- Show git blame context for edited files.

---

## P2 — Differentiation (v0.16)

### Compiler diagnostics after edits

Cheaper than a full LSP client and closes most of the practical gap. `autofix.go` already runs a linter after `edit_file`/`write_file`/`delete_file` — extend that path to `apply_diff` (currently missing from its tool map) and feed structured diagnostics from `gopls check` / `tsc --noEmit` / `cargo check` into tool results.

A full LSP client is **deprioritized** under the deployment target: language servers are memory-hungry, which is the wrong tradeoff on constrained hardware.

### Multi-source search

- Parallel search across providers (DDG + Brave + Tavily), deduplicated and ranked.
- `/fetch <url>` improvements: better extraction, PDF support.
- Domain-scoped search; result caching within a session.
- Raise the deep-read page cap (currently 3, `internal/search/search.go:15`) to a configurable 10.

### Model routing

Intent classification already exists (`internal/tui/intent.go` → `automode.go`). What remains:

- Cost-aware routing — prefer cheaper models when the task does not need reasoning depth.
- Automatic fallback: if a local model fails a tool call, retry on a cloud model.
- Per-task routing: research → Perplexity/Gemini, code → Claude/GPT, quick edits → local.

### Plugin system

- Tool definitions in YAML/JSON config: name, description, parameters, shell command.
- Project-level `.baryo/plugins/` and global `~/.baryo/plugins/`.
- Plugins can register for the existing hook lifecycle events.
- **Gated on the trust model** ([#12](https://github.com/BaryoDev/Baryo.CLI/issues/12)) — a plugin system that loads executable definitions from the working directory cannot ship before directory trust does.

### DevOps toolkit

The third pillar currently has no implementation. Either build this or drop the pillar from the header.

- `/deploy` — generate Dockerfile, compose, GitHub Actions, K8s, Terraform.
- `/docker` — manage local containers (list, build, run, stop, logs, exec).
- Infrastructure-as-code review for Terraform/CloudFormation/Pulumi.
- Log analysis — pipe container and service logs in for diagnosis. Pairs directly with the appliance target.

---

## P3 — Polish & Quality of Life (v0.17+)

### Appliance ergonomics

Serves the deployment target directly; promoted above the older polish items.

- Ship a documented systemd unit and a `baryo-appliance` example config.
- Structured run summaries for unattended jobs (exit codes that mean something, machine-readable failure reasons).
- Resource self-limiting: bound index and RAG memory on small hosts.

### Session management

- Session tagging — the `Tags` field exists on `session.Session` but nothing writes it; needs a `/tag` command.
- `/recall` over archived messages, building on `session.LoadArchive`.

### Multi-file awareness

- Surface related files (imports, tests, interfaces) when the model edits a file.
- Dependency-graph awareness: editing a function surfaces its callers.
- Test file association: editing `foo.go` surfaces `foo_test.go`.

### TUI improvements

- Keyboard shortcuts overlay (`?`).
- Better progress indicators for long tool chains.
- Inline code preview for `@mentions` before sending.
- File tree sidebar (toggleable).

Split-pane view is **deprioritized** — high effort in the least-tested package in the codebase.

### Streaming

- Streaming diff display — show edits being applied in real time.
- Interruptible tool execution — Ctrl-C cancels the current tool without killing the conversation. **Depends on the P0 cancellation fix**, which must land first.

### Documentation

- **Document the hooks system.** It shipped and has zero README mentions, so a complete feature is invisible to users.
- `baryo tutorial` — interactive walkthrough.
- `/help <topic>` with contextual examples.
- Contributing guide for plugin authors.

### Voice input

Deprioritized. Both Claude Code and Aider ship it, so it is no longer a differentiator, and it is irrelevant to unattended deployment.

---

## Completed

Items are moved here when verified present in the source tree, not when a PR merges.

### Lossless Compaction (v0.13.0)
- Pre-compaction messages archived to `~/.baryo/sessions/<id>.archive.jsonl` instead of being discarded
- `/sessions search` covers archived content
- `session.LoadArchive(id)` for full history retrieval
- Retention cleanup removes archives alongside session files
- A failed compaction stream no longer corrupts the next turn
- First tests for the `session` package

### Hooks System
Shipped, and previously still listed as planned. **Undocumented in the README** — see P3.
- Six lifecycle events: `pre_tool`, `post_tool`, `on_error`, `on_commit`, `on_stream_end`, `on_search` (`internal/config/config.go:60-73`)
- Executed via `sh -c` with a 30s timeout (`internal/tui/hooks.go`)
- Blocking: a non-zero `pre_tool` exit cancels the tool call
- Hook output surfaced in chat as tool results
- Configured in `~/.baryo/config.yaml` or `.baryo/config.yaml`

### Subagent / Task Delegation
Shipped, and previously still listed as planned.
- `/task <description>` delegates to an isolated sub-model call with its own message history
- `/bg` for background execution, `/tasks` to list
- Read-only tool executor for subagents; up to 3 concurrent (`internal/tui/subagent.go`)
- Results merged back into the main conversation
- Remaining gap: no write-capable subagents, no model-invoked `delegate_task` tool

### Session Management
Shipped, and previously still listed as planned in P3.
- Auto-generated session titles from the first user message (`session.GenerateTitle`)
- `/sessions search <query>`, covering archived messages (`session.Search`)
- Age-based auto-cleanup via `session_retention_days` (`session.CleanOld`)
- Not done: session tagging — the `Tags` field exists but nothing writes it

### Auto-Mode Intent Classification
Shipped; the roadmap previously described auto-mode as "basic tier-based routing".
- `ClassifyIntent` distinguishes Chat / Knowledge / Planning / Code (`internal/tui/intent.go`)
- Feeds `classifyTier` for model selection (`internal/tui/automode.go`)
- Remaining gap: cost-aware routing, local→cloud fallback, per-task provider routing

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
- Conversation history preserved across model switches — **regressed**, see P0: `transitionToChat` (`internal/tui/app.go:649`) rebuilds the chat model, discarding history

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
