# Baryo

An AI chat CLI for local and cloud models. Chat with models running on your machine via [Ollama](https://ollama.com) or [Docker Model Runner](https://docs.docker.com/desktop/features/model-runner/), or connect to 18+ cloud providers for inference.

Baryo provides both an interactive terminal UI and a scriptable print mode for pipelines and automation.

## Prerequisites

For local models (pick one):

- **Ollama (recommended)** — lightweight, easy to install:
  ```bash
  # macOS
  brew install ollama
  # Linux
  curl -fsSL https://ollama.com/install.sh | sh

  ollama serve
  ollama pull qwen3:0.6b
  ```
- **Docker Desktop** with [Model Runner](https://docs.docker.com/desktop/features/model-runner/) enabled:
  ```bash
  docker model pull ai/gemma3
  ```

Or skip local models entirely — cloud-only usage works with just an API key.

- [Go 1.25+](https://go.dev/dl/) (to build from source)

## Installation

### macOS / Linux (Homebrew)

```bash
brew tap BaryoDev/Baryo.CLI https://github.com/BaryoDev/Baryo.CLI
brew install baryo
```

### macOS / Linux (shell script)

```bash
curl -fsSL https://raw.githubusercontent.com/BaryoDev/Baryo.CLI/main/install.sh | sh
```

### Windows (Scoop)

```powershell
scoop bucket add baryo https://github.com/BaryoDev/Baryo.CLI
scoop install baryo
```

### Go install

```bash
go install github.com/baryodev/baryo-cli@latest
```

### Pre-built binaries

Download from [GitHub Releases](https://github.com/BaryoDev/Baryo.CLI/releases) for macOS, Linux, and Windows (amd64 + arm64).

### Build from source

```bash
git clone https://github.com/BaryoDev/Baryo.CLI.git
cd Baryo.CLI
go build -o baryo .
```

### Publishing a new release

Tag and push — GitHub Actions handles the rest:

```bash
git tag v0.1.0
git push origin main --tags
```

This automatically builds binaries for all platforms, creates a GitHub Release, and updates the Homebrew formula and Scoop manifest in this repo.

## Usage

### Interactive mode (TUI)

```bash
# Launch the TUI — pick a model, then chat
baryo

# Skip the model picker
baryo --model gemma3
```

The interactive mode gives you:
- A clean, muted terminal UI inspired by modern CLI tools — no visual clutter
- A **tabbed model picker** — Local, Groq, Gemini, Bedrock, etc. each get their own tab with model counts and pricing
- A streaming chat interface with personality — the status bar cycles through quirky fourth-wall-breaking phrases while the model thinks
- **Streaming speed metrics** — live tok/s display in the status bar while the model generates
- Structured tool call display with left-border blocks for clear visual hierarchy
- Input history — press `↑`/`↓` to cycle through previous messages
- **Shell mode** — press `Ctrl+X` to toggle direct shell command execution
- **Desktop notifications** — get notified when long-running responses complete
- Keyboard navigation (`enter` to send, `↑`/`↓` scroll, `ctrl+p`/`ctrl+n` history, `ctrl+x` shell toggle, `ctrl+c` to quit)

### Print mode

Send a prompt and get the response streamed to stdout. Useful for scripts and pipelines.

```bash
# Ask a question
baryo -p "what is 2+2"

# Use a specific model
baryo --model gemma3 -p "explain docker networking"

# Pipe file contents as context
cat main.go | baryo -p "review this code"

# Chain with other tools
baryo -p "generate a .gitignore for Go" > .gitignore
```

When stdin is piped, it's included as context alongside your prompt.

### Headless / CI mode

Print mode supports full tool calling for CI/CD pipelines and automation. The model can read files, search code, and run tools — just like in the TUI.

```bash
# Tools with auto-approve (reads files, runs grep, etc.)
baryo -p "read main.go and summarize" --yolo

# JSON structured output for pipeline consumption
baryo -p "list all Go files" --yolo --output json

# Simple Q&A without tools
baryo -p "what is 2+2" --no-tools

# Limit tool rounds for faster execution
baryo -p "review this project" --yolo --max-turns 2

# Pipe input with tool access
cat main.go | baryo -p "review this code" --yolo
```

**Output formats:**

| Format | Description |
|--------|-------------|
| `text` (default) | Tokens streamed to stdout, tool status to stderr |
| `json` | Single JSON object with content, tool calls, and usage stats |

**Exit codes:**

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Runtime error (streaming, API, tool failure) |
| 2 | Configuration error (bad model, missing endpoint) |

In text mode, tool execution status is written to stderr so stdout stays clean for piping:

```
$ baryo -p "read main.go and count the lines" --yolo
[tool] read_file          # stderr
[tool] read_file: ok      # stderr
main.go has 256 lines.    # stdout
```

Without `--yolo`, destructive tools (file writes, shell commands) are blocked and the model receives an error message suggesting `--yolo`.

### Session persistence

Conversations are automatically saved after each turn to `~/.baryo/sessions/`.

```bash
# Resume the most recent session in this directory
baryo -c

# Pick a saved session from a list
baryo -r

# Resume a specific session by ID
baryo --resume-id abc123
```

Inside the TUI you can also use:
- `/sessions` — list and pick a saved session to resume
- `/sessions search <query>` — search sessions by content
- `/resume` — alias for `/sessions`
- `/clear` — start a fresh conversation

Sessions are automatically titled from the first user message. Configure automatic cleanup of old sessions:

```yaml
# ~/.baryo/config.yaml
session_retention_days: 30   # delete sessions older than 30 days (0 = keep all)
```

### Intelligent routing

Baryo uses **two-tier routing** to match each model's capabilities. Capable models (≥32K context) get 8 meta-tools as real tool calls — they decide when to search, fetch, commit, test, and more. Smaller models get a consolidated heuristic router that triggers the right command automatically.

**Tier 1 — Capable models** (Gemini, GPT, Claude, large Llama, etc.):

The model receives meta-tools as tool definitions and calls them directly when needed — no heuristic keyword matching, no false positives.

| Tool | Type | Description |
|------|------|-------------|
| `web_search` | Safe | Search the web for current information |
| `deep_research` | Safe | Multi-round deep research with report |
| `fetch_page` | Safe | Fetch and extract content from a URL |
| `remember` | Safe | Save user preferences to persistent memory |
| `review_code` | Safe | Get current git diff for code review |
| `review_pr` | Safe | Fetch a GitHub PR (diff + comments) for review |
| `read_issue` | Safe | Read a GitHub issue (body + comments) |
| `pr_status` | Safe | Show PR review status for the repo |
| `commit_changes` | Destructive | Stage and commit with auto-generated message |
| `create_pr` | Destructive | Push branch and create a GitHub PR |
| `run_tests` | Destructive | Auto-detect test framework and run tests |
| `create_branch` | Destructive | Create and checkout a new git branch |

Safe tools work in all modes (including read-only). Destructive tools are only available in non-read-only modes and respect the permission system (`--yolo`, confirm, suggest).

```
You: What are the latest Go features?
→ model calls web_search("latest Go features") → answers with citations

You: Read this page for me: https://go.dev/blog
→ model calls fetch_page("https://go.dev/blog") → extracts and summarizes content

You: Remember that I prefer conventional commits
→ model calls remember("prefer conventional commits") → saved to memory

You: Review my changes
→ model calls review_code() → shows diff with analysis

You: Run the tests
→ model calls run_tests() → auto-detects go/npm/pytest/cargo and runs

You: Commit these changes
→ model calls commit_changes() → stages, generates message, commits

You: Create a PR for this
→ model calls create_pr() → pushes branch, opens GitHub PR

You: Review PR #42
→ model calls review_pr(42) → fetches diff + comments, analyzes

You: What's issue #5 about?
→ model calls read_issue(5) → fetches issue body + comments

You: Any PRs need my attention?
→ model calls pr_status() → shows pending reviews and status

You: Create a branch for the auth feature
→ model calls create_branch("feat/auth") → creates and checks out branch
```

**Tier 2 — Small models** (<32K context):

A single `routeInput()` function with clear priority ordering replaces scattered detection paths:

```
research golang vs rust for web servers    → auto-triggers /research
deep dive into kubernetes networking       → auto-triggers /research
investigate the performance regression     → auto-triggers /research
what are the latest Go features?           → auto-triggers /search (freshness keyword)
should I use PostgreSQL or MongoDB?        → offers /strategy (y/n)
what's the best approach to scaling?       → offers /strategy (y/n)
```

**Both tiers** share conversational shortcuts (saying "yes" after a search suggestion), post-stream safety nets, and all slash commands. Switching models mid-session via `/models` changes the routing tier automatically.

### Slash commands

Baryo also includes explicit slash commands for when you want direct control. Type `/help` to see them all.

| Command | Description |
|---------|-------------|
| `/help` | List all available commands |
| `/diff` | Show current git diff in chat |
| `/commit` | Generate a commit message from staged changes and auto-commit |
| `/review` | Review current git diff for bugs, style issues, and improvements |
| `/undo` | Undo the last git commit (soft reset, changes stay staged) |
| `/run <cmd>` | Run a shell command and display output |
| `/ask <question>` | Ask the model without tool access (fast, read-only) |
| `/search <query>` | Search the web and summarize results |
| `/research <topic>` | Multi-round deep research with structured report |
| `/fetch <url>` | Fetch and display a web page |
| `/test [path]` | Run project tests (auto-detects go/npm/pytest/cargo) |
| `/pr [title]` | Push current branch and create a GitHub PR |
| `/pr review [number]` | Review a PR (fetches diff + comments, streams analysis) |
| `/pr status` | Show PR review status for the repo |
| `/issue <number>` | Read a GitHub issue and get implementation suggestions |
| `/branch <name>` | Create and checkout a new git branch |
| `/setup` | Download or update starter skills from GitHub |
| `/skills` | List available skills |
| `/skill <name>` | Activate a skill (loads full instructions into context) |
| `/clear` | Start a fresh conversation |
| `/sessions` | List and pick a saved session |
| `/models` | Browse and switch models |
| `/init` | Generate a BARYO.md for this project |
| `/system [prompt]` | View or change the system prompt |
| `/params [k=v]` | View or change model parameters |
| `/plan <prompt>` | Enter plan mode (read-only tools, produces implementation plan) |
| `/plan done` | Exit plan mode and restore normal tool access |
| `/strategy` | Start an interactive strategy wizard for structured decision analysis |
| `/strategy <file>` | Load a JSON strategy file for analysis |
| `/strategy init` | Generate a blank strategy.json template |
| `/strategy done` | Exit strategy mode |
| `/mode [name]` | Switch agent mode or list all modes |
| `/mcp` | List connected MCP servers and their tools |
| `/context` | Show token usage breakdown |
| `/cost` | Show session API cost (cloud providers) |
| `/compact` | Summarize older messages to free context |
| `/pin <file>` | Pin a file to context (re-read each turn) |
| `/unpin <file>` | Unpin a file from context |
| `/pins` | List pinned files |
| `/checkpoint <name>` | Save conversation state as a named checkpoint |
| `/rewind [name]` | List checkpoints or rewind to one |
| `/task <desc>` | Run a focused sub-task (read-only agent) |
| `/bg <desc>` | Run a background sub-task |
| `/tasks` | Show running and completed tasks |
| `/new <type>` | Scaffold a new project (go-api, react-app, etc.) |
| `/export [file]` | Export conversation to a file |
| `/copy` | Copy last response to clipboard |
| `/markdown` | Toggle markdown rendering |
| `/doctor` | Run diagnostic checks |

**Workflows:**

```bash
# Issue → Branch → Code → Test → Commit → PR
/issue 42               # Read the issue, get implementation plan
/branch feat/issue-42   # Create a feature branch
# ... implement the feature ...
/test                    # Run tests to verify
/review                  # Review your local changes
/commit                  # Generate a commit message and commit
/pr "feat: add user auth"  # Push and create a PR

# Review someone else's PR
/pr review 15            # Fetch PR #15 diff + comments, stream analysis
/pr status               # Check pending reviews across the repo

# Run tests for a specific path
/test ./internal/tui/...

# Quick question without tool overhead
/ask explain what a goroutine is
```

### Model browser

Browse downloaded and available models from Docker Hub without leaving the TUI.

```bash
# Inside the TUI
/models
```

The model browser uses the same **tabbed interface** as the model picker — Local, cloud providers, and Docker Hub each get their own tab. Navigate with `Tab`/`←`/`→` between tabs and `↑`/`↓` within. Select a downloaded model to switch to it, or select a Docker Hub model to pull it with live progress.

### Hardware fit tags

The model picker and browser show a colored fit tag next to each local model, so you know at a glance whether a model will run well on your machine.

```
▸ ai/gemma3                                          fast
  params: 4 B  size: 2.34 GiB — plenty of room, expect fast responses

  ai/llama3.1:70b                                    too large
  params: 70.6 B  size: 38.5 GiB — likely won't fit, expect crashes or heavy swapping
```

| Tag | Memory usage | What to expect |
|-----|-------------|----------------|
| **fast** | <60% of RAM | Plenty of room, expect fast responses |
| **smooth** | 60-80% | Fits well, may slow during long contexts |
| **slow** | 80-95% | Tight fit, expect slower responses and possible swapping |
| **too large** | >95% | Likely won't fit, expect crashes or heavy swapping |

Memory is estimated from the model's on-disk size (or parameter count as fallback) and compared against your system's total RAM. Cloud provider models skip fit scoring entirely — they show pricing instead.

### Startup diagnostics

Baryo checks your Docker setup on every launch. If cloud providers are configured and Docker isn't available, it prints a warning and continues — the model picker just won't have a Local tab. If neither Docker nor any cloud provider is configured, it exits with step-by-step instructions.

```bash
# Run the full diagnostic manually
baryo doctor

# Skip checks for faster startup
baryo --skip-checks
```

The checks run in order:
1. Docker installed
2. Docker running
3. Model Runner enabled (inference socket exists)
4. At least one model pulled

You can also run `/doctor` inside the TUI to check diagnostics mid-session.

### Markdown rendering

Assistant responses are rendered with full markdown formatting by default, including syntax-highlighted code blocks. Toggle it on or off mid-session:

```bash
# Inside the TUI
/markdown
```

When enabled, code blocks display with syntax highlighting and text is formatted with headings, lists, and emphasis. Disable it with `/markdown` again to see raw plain text.

### Context management

Baryo tracks estimated token usage and shows it in the status bar.

```
enter send · ↑↓ scroll · ctrl+p/n history · ctrl+c quit          ~3.2k / 8k
```

The token count is color-coded: dim when under 60%, amber at 60-85%, and red above 85%.

**Commands:**

| Command | Description |
|---------|-------------|
| `/context` | Show a detailed breakdown of token usage (system prompt, conversation, total) |
| `/compact` | Summarize older messages to free context space, keeping the last 4 exchanges |

**Auto-compaction** triggers automatically when token usage exceeds 85% of the context limit and there are enough messages to compact. Older messages are replaced with a summary while recent exchanges are kept verbatim.

### Knowledge retrieval (RAG)

Baryo automatically retrieves relevant context from local knowledge files and past conversations, injecting it into the system prompt so the model answers better without extra effort.

**Knowledge files:** Place `.md`, `.txt`, or `.rst` files in a knowledge directory:

| Path | Scope |
|------|-------|
| `~/.baryo/knowledge/` | Global — available in all projects |
| `.baryo/knowledge/` | Project-local — only for this project |

```bash
# Add project-specific knowledge
mkdir -p .baryo/knowledge
echo "Our API runs on port 8443. Deploy via GitHub Actions." > .baryo/knowledge/infra.md

# Add global knowledge
mkdir -p ~/.baryo/knowledge
echo "Always use conventional commits: feat, fix, chore, docs." > ~/.baryo/knowledge/conventions.md
```

**How it works:**

1. On startup, Baryo indexes knowledge files, up to 20 recent sessions, and project source files in the background
2. On each user message, BM25 keyword search ranks the most relevant chunks across all three stores
3. Matching content is injected as a `<context>` block in the system prompt (with `<sources>`, `<documents>`, and `<sessions>` sections)
4. Budget scales with context window: 0% for <16K, 3-10% for larger windows

**Source file indexing:** Baryo automatically indexes your project's source files (`.go`, `.ts`, `.py`, `.rs`, `.java`, `.rb`, `.c`, `.cpp`, and more). When you ask about your codebase, relevant code chunks are included in the context — no need to manually `@`-mention files. If tree-sitter parsing is available (CGO build), chunks are split at symbol boundaries (one chunk per function/type); otherwise, line-based chunking is used as a fallback. Up to 500 files are indexed, with code files prioritized over config/docs.

**Session memory:** Past conversations are automatically indexed. If you discussed something in a previous session, relevant Q&A pairs surface as context for new questions.

**Auto web search:** Time-sensitive queries (containing "today", "latest", "2025", "news about", etc.) automatically trigger `/search` before the model responds — no more two-step "I don't know" → "yes, search" dance.

**Check status:**

```bash
/context    # shows RAG line alongside repo map and token counts
```

```
System prompt:   ~2.1k tokens
Conversation:    ~800 tokens (4 messages)
Repo map:        ~1.5k tokens (42 files)
RAG:             ~350 tokens (85 sources, 3 docs, 12 sessions)
Total estimated: ~4.8k / 128k (3%)
```

RAG is skipped entirely for small models (<16K context) to avoid wasting limited space.

### Conversation export

Export your conversation to a file or copy the last response to the clipboard.

```bash
# Inside the TUI

# Export as markdown (default)
/export

# Export with a custom filename
/export my-chat.md

# Export as JSON (array of messages)
/export chat.json

# Copy last assistant response to clipboard
/copy
```

- `/export` with no argument creates `baryo-export-<timestamp>.md` in the current directory
- Filenames ending in `.json` produce a JSON array of `{role, content}` objects
- All other filenames produce a markdown document with `### User` / `### Assistant` sections

### System prompts

Baryo ships with a default system prompt. You can override it per-session, in config, or via environment variable.

```bash
# One-off override
baryo --system-prompt "You are a Go expert. Be terse."

# View the active system prompt in the TUI
/system

# Change it mid-session
/system You are a Python expert.
```

Set a persistent system prompt in your config file:

```yaml
# ~/.baryo/config.yaml
system_prompt: "You are a senior engineer. Be concise and precise."
```

Or via environment variable: `BARYO_SYSTEM_PROMPT`.

### Model parameters

Control inference behavior with temperature, top-p, and max tokens.

```bash
# Set via CLI flags
baryo --temperature 0.8 --top-p 0.9 --max-tokens 2048 -p "write a poem"

# View current params in the TUI
/params

# Adjust mid-session
/params temperature=1.2 max_tokens=4096
```

Set persistent defaults in your config file:

```yaml
# ~/.baryo/config.yaml
params:
  temperature: 0.7
  top_p: 0.9
  max_tokens: 2048
```

### Flags

| Flag | Description |
|------|-------------|
| `-p <prompt>` | Send a prompt in non-interactive (print) mode |
| `--model <name>` | Select a model by name or substring |
| `--system-prompt <s>` | Override the system prompt |
| `--temperature <f>` | Sampling temperature (0.0-2.0) |
| `--top-p <f>` | Nucleus sampling threshold (0.0-1.0) |
| `--max-tokens <n>` | Maximum tokens to generate |
| `-c`, `--continue` | Resume the most recent session in this directory |
| `-r`, `--resume` | List and pick a saved session to resume |
| `--resume-id <id>` | Resume a specific session by ID |
| `--tunnel <user@host>` | Auto-start SSH tunnel to remote server |
| `-y`, `--yolo` | Auto-approve all destructive tool calls |
| `--max-turns <n>` | Max tool-call rounds in print mode (default: 5) |
| `--output <fmt>` | Output format for print mode: `text` or `json` |
| `--no-tools` | Disable tool calling in print mode |
| `--strategy <file>` | Path to strategy JSON file (use with `-p`) |
| `--worktree` | Run in an isolated git worktree |
| `--sandbox` | Run code in Docker sandbox containers |
| `--debug` | Enable debug logging to `~/.baryo/debug.log` |
| `--skip-checks` | Skip startup health checks |
| `--version` | Print version and exit |
| `--help` | Print usage and exit |
| `doctor` | Run full diagnostic check (subcommand) |
| `completion <shell>` | Generate shell completion script (zsh, bash, fish, powershell) |

### Model matching

The `--model` flag uses smart matching:

1. **Exact match** — `--model ai/gemma3`
2. **Short name** — `--model gemma3` (matches `ai/gemma3`)
3. **Substring** — `--model gem` (matches `ai/gemma3`)

If the query is ambiguous, you'll see the list of matching models. If nothing matches, you'll see all available models.

## Configuration

Baryo supports layered configuration through YAML files and environment variables.

### Config files

Create a YAML config file at either location:

- **User-level:** `~/.baryo/config.yaml`
- **Project-level:** `.baryo/config.yaml` (overrides user config)

```yaml
# ~/.baryo/config.yaml
model: ai/gemma3
socket_path: ~/Library/Containers/com.docker.docker/Data/inference.sock
system_prompt: "You are a helpful assistant. Be concise."
```

### Environment variables

| Variable | Description |
|----------|-------------|
| `BARYO_MODEL` | Default model |
| `BARYO_SOCKET` | Docker Model Runner socket path |
| `BARYO_SYSTEM_PROMPT` | Override the system prompt |
| `BARYO_GEMINI_API_KEY` | Google Gemini API key |
| `BARYO_OPENAI_API_KEY` | OpenAI API key |
| `BARYO_ANTHROPIC_API_KEY` | Anthropic API key |
| `BARYO_BEDROCK_REGION` | AWS Bedrock region (uses AWS credential chain) |
| `BARYO_GROQ_API_KEY` | Groq API key |
| `BARYO_OPENROUTER_API_KEY` | OpenRouter API key |
| `BARYO_DEEPSEEK_API_KEY` | DeepSeek API key |
| `BARYO_XAI_API_KEY` | xAI (Grok) API key |
| `BARYO_MISTRAL_API_KEY` | Mistral API key |
| `BARYO_COHERE_API_KEY` | Cohere API key |
| `BARYO_HUGGINGFACE_API_KEY` | Hugging Face API token |
| `BARYO_GITHUB_TOKEN` | GitHub personal access token (needs `models:read` scope) |
| `BARYO_OLLAMA_API_KEY` | Ollama Cloud API key |
| `DOCKER_MODEL_SOCKET` | Docker Model Runner socket path (legacy) |
| `BARYO_AUTO_LINT` | Enable auto-lint after code edits (`true`/`1`/`yes`) |
| `BARYO_AUTO_TEST` | Enable auto-test after code edits (`true`/`1`/`yes`) |
| `BARYO_LINT_COMMAND` | Custom lint command override |
| `BARYO_TEST_COMMAND` | Custom test command override |
| `BARYO_NOTIFICATIONS` | Enable desktop notifications (`true`/`1`/`yes`) |
| `BARYO_SESSION_RETENTION_DAYS` | Auto-delete sessions older than N days |
| `BARYO_SANDBOX` | Enable sandboxed code execution (`true`/`1`/`yes`) |

### Precedence

Configuration is merged in this order (highest priority wins):

```
CLI flags > Environment variables > Project config > User config > Defaults
```

### Default socket paths

| Platform | Path |
|----------|------|
| macOS | `~/Library/Containers/com.docker.docker/Data/inference.sock` |
| Linux | `~/.docker/desktop/inference.sock` (probes multiple paths) |
| Windows | `//./pipe/docker_model_runner` |

On Linux, Baryo probes `~/.docker/desktop/inference.sock`, `~/.docker/inference.sock`, and `/var/run/docker/inference.sock`, using the first that exists.

TCP connections are also supported for cross-platform use:

```yaml
# ~/.baryo/config.yaml
socket_path: "tcp://localhost:12434"
```

Or via environment variable: `DOCKER_MODEL_SOCKET=tcp://localhost:12434`.

### Remote servers (SSH tunnel)

Connect to a remote server running Ollama or any OpenAI-compatible API. Baryo auto-launches an SSH tunnel and tears it down on exit.

**Quick start (CLI flag):**

```bash
# Connect to a remote Ollama server (defaults: SSH port 22, Ollama port 11434)
baryo --tunnel user@your.server.ip --model qwen3-coder:latest

# Custom SSH port
baryo --tunnel user@myserver.com:2222 --model llama3.2
```

**Persistent config (YAML):**

```yaml
# ~/.baryo/config.yaml
model: "qwen3-coder:latest"

ssh_tunnel:
  host: "your.server.ip"
  user: "your-user"
  remote_port: 11434   # port on the remote server
  local_port: 11434    # local port to forward to
  # ssh_port: 22       # SSH port (default: 22)
```

When `ssh_tunnel` is present (or `--tunnel` is used), Baryo will:

1. Check if the local port is already open (skip spawning if so)
2. Run `ssh -N -L local:localhost:remote user@host`
3. Wait up to 15 seconds for the port to become reachable
4. Set the socket path to `tcp://localhost:<local_port>`
5. Kill the SSH process on exit (Ctrl+C or normal quit)

The `--model` flag is required when using a remote connection since Baryo can't run `docker model list` on the remote server.

Run diagnostics on a remote connection:

```bash
baryo --tunnel user@your.server.ip doctor
```

### @ Mentions

Attach file contents as context by typing `@filepath` in your message. Tab completion helps you find files quickly.

```bash
# Type @ then Tab to autocomplete
@main.go           # attach a single file
@internal/tui/     # Tab to browse subdirectory contents

# Multiple files in one message
explain @main.go @go.mod

# Attach images for vision models
explain @screenshot.png
compare @before.png @after.png

# Quoted paths for files with spaces
@"path/to/my file.png"
@'notes with spaces.md'

# Absolute and home-relative paths for images
@~/Desktop/screenshot.png
@/tmp/diagram.png

# Recursive matching — typing @cha finds internal/tui/chat.go
@cha               # shows matches across all subdirectories
```

**How it works:**

- As you type after `@`, matching files appear in the status bar
- **Tab** / **Shift+Tab** — cycle through candidates
- **Enter** — select the highlighted file (inserts it into your input)
- **Esc** — dismiss suggestions
- Press **Enter** again to send — file contents are injected as context for the model

Text files must be under 100KB, non-binary, and not gitignored. **Images** (`.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`, `.svg`) are base64-encoded and sent as vision content — up to 20MB per image, max 4 images per message. Paths with spaces can be quoted (`@"my file.png"` or `@'my file.png'`), and `~/` paths are expanded for images outside the project. If the current model doesn't appear to support vision, a warning is shown when images are attached. Directories can be browsed via tab completion but not attached directly. Duplicate mentions are deduplicated.

### Web search

Search the web and get AI-summarized results — no API key needed (DuckDuckGo is the default).

```bash
# Inside the TUI
/search latest news philippines
/search golang error handling best practices
```

**How it works:**

1. Searches via DuckDuckGo (or Brave/Tavily if configured)
2. Auto-fetches the top result pages for actual article content
3. The model summarizes everything into a clean paragraph with inline source citations
4. Ends with "Want me to search for more or dive deeper?"

The model is instructed to be honest about uncertainty. If you ask something it doesn't know, it will search or research automatically:

```
You: What happened in the Senate hearing today?
→ auto-triggers /search (current events, quick factual lookup)

You: research the impact of AI on healthcare
→ auto-triggers /research (deep investigation, multi-source analysis)

You: What's the latest on the Mars mission?
Assistant: I don't have current information about that, let me search for you.
→ auto-triggers /search from the model's suggestion
```

**Fetch a specific page:**

```bash
/fetch https://example.com/article
```

**Configure a different provider:**

```yaml
# ~/.baryo/config.yaml
search_provider: brave    # or tavily
search_api_key: your-key
```

### Deep research

Run multi-round research on a topic. Baryo searches iteratively, identifies knowledge gaps, fetches follow-up sources, and compiles a structured report with citations.

```bash
# Inside the TUI
/research quantum computing advances
/research quick Go error handling          # 1 round (fast)
/research deep Rust vs Go for web servers  # 5 rounds (thorough)
```

**Depth levels:**

| Prefix | Rounds | Use case |
|--------|--------|----------|
| `quick` | 1 | Enhanced search with a structured report |
| *(default)* | 3 | Balanced depth for most topics |
| `deep` | 5 | Comprehensive investigation |

**How it works:**

1. Each round: searches the web, fetches top pages, and asks the model to analyse findings
2. The model identifies knowledge gaps and generates follow-up search queries
3. Next round searches those follow-up queries for deeper coverage
4. Progress updates appear in the status bar as each round runs
5. After all rounds, the model compiles a structured report with an executive summary, key findings, analysis, and numbered source citations

The report is a normal assistant message — use `/copy` to clipboard or `/export` to save it. Follow up naturally in conversation ("dig deeper into finding #3") since the full report stays in context.

**Context-aware:** Research automatically scales to fit your model's context window. Findings are compacted per-round and trimmed if needed so the final report prompt doesn't overflow.

### Cloud providers

Baryo supports 15+ cloud providers alongside local Docker models. Configure an API key and models appear in their own tab in the picker. Docker is optional — cloud-only usage works fine.

**Supported providers:**

| Provider | Config key | Environment variable | Model prefix |
|----------|-----------|---------------------|-------------|
| [Google Gemini](https://ai.google.dev/) | `gemini` | `BARYO_GEMINI_API_KEY` | `gemini-` |
| [OpenAI](https://platform.openai.com/) | `openai` | `BARYO_OPENAI_API_KEY` | `gpt-`, `o1`, `o3` |
| [Anthropic](https://console.anthropic.com/) | `anthropic` | `BARYO_ANTHROPIC_API_KEY` | `claude-` |
| [AWS Bedrock](https://aws.amazon.com/bedrock/) | `bedrock` | `BARYO_BEDROCK_REGION` | `anthropic.`, `amazon.`, `meta.` |
| [Groq](https://console.groq.com/) | `groq` | `BARYO_GROQ_API_KEY` | `llama-`, `gemma-` |
| [OpenRouter](https://openrouter.ai/) | `openrouter` | `BARYO_OPENROUTER_API_KEY` | — |
| [Mistral](https://console.mistral.ai/) | `mistral` | `BARYO_MISTRAL_API_KEY` | `mistral-` |
| [DeepSeek](https://platform.deepseek.com/) | `deepseek` | `BARYO_DEEPSEEK_API_KEY` | `deepseek-` |
| [xAI](https://console.x.ai/) | `xai` | `BARYO_XAI_API_KEY` | `grok-` |
| [Cohere](https://dashboard.cohere.com/) | `cohere` | `BARYO_COHERE_API_KEY` | `command-` |
| [Together](https://api.together.xyz/) | `together` | `BARYO_TOGETHER_API_KEY` | — |
| [Fireworks](https://fireworks.ai/) | `fireworks` | `BARYO_FIREWORKS_API_KEY` | — |
| [Cerebras](https://cloud.cerebras.ai/) | `cerebras` | `BARYO_CEREBRAS_API_KEY` | — |
| [Perplexity](https://docs.perplexity.ai/) | `perplexity` | `BARYO_PERPLEXITY_API_KEY` | `sonar` |
| [SambaNova](https://cloud.sambanova.ai/) | `sambanova` | `BARYO_SAMBANOVA_API_KEY` | — |
| [Hugging Face](https://huggingface.co/docs/inference-providers/) | `huggingface` | `BARYO_HUGGINGFACE_API_KEY` | `org/model` |
| [GitHub Models](https://docs.github.com/en/github-models) | `github` | `BARYO_GITHUB_TOKEN` | `publisher/model` |
| [Ollama Cloud](https://ollama.com/) | `ollama` | `BARYO_OLLAMA_API_KEY` | — |

**Tested providers:** The following have been verified working end-to-end (model listing, streaming chat, tool calls):

| Provider | Models tested |
|----------|--------------|
| Groq | `llama-3.3-70b-versatile`, `gemma2-9b-it` |
| Cerebras | `llama-3.3-70b` |
| Cohere | `command-a-03-2025` |
| Hugging Face | `deepseek-ai/DeepSeek-V3.2`, `Qwen/Qwen3-8B`, `Qwen/Qwen3.5-35B-A3B` |
| Gemini | `gemini-2.5-flash` |
| GitHub Models | `openai/gpt-4.1-nano`, `deepseek/DeepSeek-R1` |

Other providers follow the same OpenAI-compatible pattern and should work, but haven't been individually verified. If you test one, feel free to open a PR updating this list.

**Hugging Face** uses the [Inference Providers](https://huggingface.co/docs/inference-providers/) router API. Free accounts get $0.10/month in credits. Model IDs use `org/model` format (e.g. `deepseek-ai/DeepSeek-V3.2`). No prefix-based auto-detection — select HF models from the model picker tab.

**GitHub Models** uses a [GitHub PAT](https://github.com/settings/tokens) with `models:read` scope. Free tier rate limits depend on your GitHub plan. Gives access to 30+ models (GPT-4o, DeepSeek-R1, Llama, Mistral, etc.) — all free. No prefix-based auto-detection — select models from the picker tab.

All providers use the unified `provider_keys` map:

```yaml
# ~/.baryo/config.yaml
provider_keys:
  groq: gsk_your_key
  gemini: your-gemini-key
  anthropic: sk-ant-your-key
  bedrock: us-east-1          # region, not an API key — uses AWS credential chain
```

Or via environment variables:

```bash
export BARYO_GROQ_API_KEY=gsk_your_key
export BARYO_GEMINI_API_KEY=your-key
```

**AWS Bedrock** is special — the config value is a region (e.g. `us-east-1`), not an API key. Credentials come from the standard AWS chain (`AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`, `~/.aws/credentials`, IAM roles, SSO).

**Cost tracking:** For cloud providers, the status bar shows your cumulative session cost next to the token count. Use `/cost` to see the current session spend.

```
enter send · ↑↓ scroll · ctrl+p/n history · ctrl+c quit    ~1.2k / 8k · $0.0012
```

### Tool use

Baryo gives the model access to local tools so it can read files, search code, explore your project, and check git state.

Available tools:

| Tool | Description |
|------|-------------|
| `read_file` | Read the contents of a file |
| `glob` | Find files matching a pattern (supports `**`) |
| `grep` | Search file contents by regex |
| `list_directory` | List directory contents as a tree |
| `git_status` | Show modified, staged, and untracked files |
| `git_diff` | Show file diffs (staged or unstaged) |
| `git_log` | Show recent commit history |
| `gh` | Run read-only GitHub CLI commands (PRs, issues, releases) |

Tools are called automatically when the model decides they're needed. Results are shown inline in the chat with an animated spinner while executing. The `.git` directory is excluded from file listings, and `.gitignore` rules are respected.

The `gh` tool requires the [GitHub CLI](https://cli.github.com/) to be installed and is restricted to read-only operations (list, view, diff, checks, status).

Models that support the native OpenAI tool-calling API will use it directly. For models that don't, Baryo includes a text-based fallback parser that detects tool calls in the model's output and executes them transparently.

If a model explicitly rejects tool use (e.g. Cohere's `c4ai-aya-*` models, or models that return "tool calling is not supported"), Baryo automatically retries the request without tools and disables them for the rest of the session. You'll see an info message in the chat and subsequent messages skip tools entirely — no repeated errors.

### Auto-fix on lint/test

When enabled, Baryo automatically runs your project's linter and/or tests after every code-modifying tool call (`edit_file`, `write_file`, `delete_file`). Errors are appended to the tool result so the model sees them immediately and self-corrects.

```yaml
# ~/.baryo/config.yaml or .baryo/config.yaml
auto_lint: true
auto_test: true
```

**Auto-detection:** Baryo detects the right commands based on your project:

| Project marker | Lint command | Test command |
|---------------|--------------|--------------|
| `go.mod` | `golangci-lint run` (fallback: `go vet ./...`) | `go test -short ./...` |
| `package.json` | `npx eslint .` | `npx jest --bail` |
| `Cargo.toml` | `cargo clippy` | `cargo test` |
| `pyproject.toml` | `python -m flake8` | `python -m pytest --tb=short -q` |

**Custom commands:** Override auto-detection with your own commands:

```yaml
auto_lint: true
lint_command: "golangci-lint run --fix"
auto_test: true
test_command: "go test -count=1 ./..."
```

Each command has a 30-second timeout. Output is truncated to 4000 characters. Both options default to `false`.

| Variable | Description |
|----------|-------------|
| `BARYO_AUTO_LINT` | Enable auto-lint (`true`/`1`/`yes`) |
| `BARYO_AUTO_TEST` | Enable auto-test (`true`/`1`/`yes`) |
| `BARYO_LINT_COMMAND` | Custom lint command |
| `BARYO_TEST_COMMAND` | Custom test command |

### Plan mode

Enter a read-only analysis mode where the model explores your codebase and produces an implementation plan before any code changes happen.

```bash
# Inside the TUI
/plan refactor the search package to support pagination
```

In plan mode the model has access to read-only tools (`read_file`, `glob`, `grep`, `list_directory`, `git_status`, `git_diff`, `git_log`) but cannot write files, run shell commands, or execute code. The header bar shows **plan** while the mode is active.

Exit plan mode and restore normal tool access:

```bash
/plan done
```

Plan mode also resets automatically on `/clear`.

### Strategy planning

Structured decision analysis for complex choices — career moves, purchases, business strategy, and anything where you need to weigh trade-offs.

```bash
# Inside the TUI
/strategy
```

The strategy wizard interviews you step-by-step: your goal, relevant facts, constraints, and context. When you say "done", it produces a structured analysis with an optimal strategy, reasoning, sensitivity analysis, and knowledge gap searches.

**Auto-detection:** Baryo detects strategy-worthy questions automatically. If you type something like "should I buy a Toyota or Honda for my family?" or "what's the best approach to paying off student loans?", it will offer to enter strategy mode:

```
You: should I buy a Toyota or Honda for my family?
ℹ This looks like a strategic decision. Use /strategy for structured analysis? (y/n)
y → enters strategy wizard with your question as the goal
n → responds normally as a regular chat message
```

Detection triggers on decision phrases ("should I", "which is better", "help me decide"), trade-off phrases ("pros and cons", "compare", "vs"), and strategy phrases ("best approach", "best way to").

**JSON input:** For repeatable or complex analyses, define your inputs in a file:

```bash
/strategy init              # generate a blank template
# edit strategy.json with your goal, facts, and constraints
/strategy strategy.json     # load and analyze
```

**Knowledge gap searches:** The analysis includes `/search` queries for information gaps. Baryo auto-runs these searches and feeds the results back into a refined analysis.

**Exit strategy mode:**

```bash
/strategy done
```

### Agent modes

Switch between specialized modes that control tool access and model behavior. Each mode is color-coded in the status bar.

```bash
/mode         # list all modes with current highlighted
/mode code    # switch to code mode
/mode chat    # return to default
```

| Mode | Tools | Description |
|------|-------|-------------|
| `chat` | Dynamic | Default — tools used when the model decides they're needed |
| `ask` | None | Fast answers with no tool access |
| `code` | All | Full tool access on every message |
| `architect` | Read-only | Explore codebase and plan without making changes |
| `review` | Read-only | Code review focus with read-only tools |
| `research` | Read-only | Web search and exploration |

Destructive commands (`/run`, `/commit`, `/init`, `/test`, `/pr`) are blocked in restricted modes. The model receives a system message on each mode switch so it adjusts behavior immediately.

### MCP support

Connect external tool servers via the [Model Context Protocol](https://modelcontextprotocol.io/) standard. MCP lets you plug in GitHub, databases, Slack, and any other MCP-compatible service as additional tools.

**Configure servers** in `~/.baryo/config.yaml` or `.baryo/config.yaml`:

```yaml
mcp_servers:
  - name: filesystem
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/home/user"]
  - name: github
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env: ["GITHUB_PERSONAL_ACCESS_TOKEN=ghp_xxx"]
```

MCP tools appear alongside built-in tools transparently — the model can call them the same way it calls `read_file` or `grep`. Use `/mcp` inside the TUI to list connected servers and their available tools.

MCP works in both interactive and headless (`-p`) modes. Failed server connections are non-fatal; the app continues without them and logs a warning.

### Project instructions

Baryo loads project-specific instructions from `BARYO.md` files and injects them into the system prompt. This lets you customize the model's behavior per-project.

Baryo checks these locations (all are optional, all found are combined):

| File | Scope |
|------|-------|
| `BARYO.md` | Project root — project-specific instructions |
| `.baryo/BARYO.md` | Project config directory — alternative location |
| `~/.baryo/BARYO.md` | User home — global instructions for all projects |

Use the `/init` command inside the TUI to generate a `BARYO.md`. The model reads your project files (README, config files, directory structure, recent commits) and writes tailored instructions automatically.

### Skills

Baryo includes a starter skill pack and supports community skills based on the [Anthropic Agent Skills](https://github.com/anthropics/skills) format. Skills extend the model with domain-specific knowledge, scripts, and code execution capabilities.

**First-run setup:** On first launch, Baryo asks if you'd like to download 5 starter skills (code review, code generation, refactoring, debugging, documentation). Press `y` to install them to `~/.baryo/skills/`, or `n` to skip. You can always run `/setup` later to download or update them.

```bash
# Download or update starter skills anytime
/setup
```

The `/setup` command is smart about updates — it tracks file checksums and skips any skills you've customized, so your edits are never overwritten.

**Auto-activation:** Skills activate automatically when your message matches trigger keywords. Ask "create an excel file" and the `code-generation` skill loads instantly — no manual commands needed.

![Skill auto-activation](assets/skill-auto-activation.png)

**Manual activation:**

```bash
/skills              # List all available skills
/skill pdf           # Activate a specific skill
```

**Starter skills** (installed via `/setup`):

| Skill | Description |
|-------|-------------|
| `code-review` | Review code for bugs, security, performance, and style |
| `code-generation` | Generate production code from requirements, following project conventions |
| `refactoring` | Named refactoring patterns with behavior-preserving transforms |
| `debugging` | Systematic debugging: reproduce, hypothesize, isolate, fix, verify |
| `documentation` | READMEs, API docs, inline comments, architecture docs |

**Additional skills** (install manually from [anthropic/skills](https://github.com/anthropics/skills)):

| Skill | Description |
|-------|-------------|
| `pdf` | Read, merge, split, OCR, fill forms, create PDFs |
| `docx` | Create, read, edit Word documents |
| `pptx` | Create and edit PowerPoint presentations |
| `xlsx` | Create, edit, analyze spreadsheets and financial models |
| `slack-gif-creator` | Create animated GIFs optimized for Slack |
| `frontend-design` | Build distinctive, production-grade web interfaces |
| `algorithmic-art` | Generative art with p5.js and seeded randomness |
| `canvas-design` | Visual art and poster design in PNG/PDF |
| `mcp-builder` | Create MCP servers for LLM tool integration |
| `doc-coauthoring` | Structured workflow for co-authoring documentation |
| `webapp-testing` | Test web apps with Playwright |
| `web-artifacts-builder` | Multi-component React/Tailwind HTML artifacts |
| `skill-creator` | Create and benchmark new skills |
| `theme-factory` | Apply professional themes to artifacts |
| `internal-comms` | Templates for newsletters, updates, FAQs |
| `brand-guidelines` | Anthropic brand colors and typography |

**Code execution:** Skills with scripts get `run_code` and `run_script` tools. The model writes code, executes it, and reports results — including the file path so you can find the output.

```
You: create a spreadsheet to track expenses

[xlsx skill auto-activated]
Tool: run_code(python) → Files created: output_files/expense_tracker.xlsx
```

Output files are saved to the `output_files/` directory in your project root.

**Skill structure:**

```
my-skill/
├── SKILL.md          # Required — YAML frontmatter + instructions
├── scripts/          # Optional — executable scripts (.py, .sh, .js)
├── core/             # Optional — importable modules
└── resources/        # Optional — supporting files
```

**Skill directories are scanned from:**

| Path | Scope |
|------|-------|
| `skills/*/SKILL.md` | Project root |
| `.baryo/skills/*/SKILL.md` | Project config directory |
| `~/.baryo/skills/*/SKILL.md` | Global skills for all projects |

Skills are **lazy-loaded** — only names and descriptions are indexed on startup. Full content, scripts, and resources are loaded on-demand when activated. Project-level skills override global skills with the same name.

**Install skills:**

```bash
# Download starter skills automatically
/setup

# Or install additional skills manually
cp -r skills/pdf ~/.baryo/skills/pdf
```

### Context pinning

Pin files to the conversation context so they're re-read on every turn. Useful for keeping key files visible as you iterate.

```bash
/pin main.go          # pin a file
/pin @internal/tui/   # @ prefix is stripped automatically
/unpin main.go        # remove from pinned context
/pins                 # list all pinned files
```

Pinned files are re-read each turn (catching edits) and injected into the system prompt. A warning appears if pinned content exceeds 25% of the context limit. Pins survive `/clear` but not app restart.

### Checkpoints & rewind

Save snapshots of your conversation state and rewind to them.

```bash
/checkpoint before-refactor    # save current state
# ... make changes ...
/rewind                        # list all checkpoints
/rewind before-refactor        # restore that checkpoint
```

Checkpoints capture the full message history and git HEAD hash. Rewind restores the conversation to that point. The git hash is shown so you can manually `git checkout` if needed.

### Background agents

Run sub-tasks in the background while you continue chatting.

```bash
/task summarize internal/tui/chat.go   # focused read-only sub-task
/bg research golang error handling     # background sub-task
/tasks                                 # show running and completed tasks
```

Background tasks run concurrently and notify you when complete (if notifications are enabled). Up to 3 concurrent sub-tasks are supported.

### Project scaffolding

Generate new projects from templates using the model's knowledge.

```bash
/new go-api        # scaffold a Go API project
/new react-app     # scaffold a React app
/new               # list available project types
```

Supported types: `go-api`, `go-cli`, `react-app`, `next-app`, `python-cli`, `python-api`, `node-api`, `rust-cli`. The model generates all necessary files using the `write_file` tool.

### Shell completions

Generate shell completion scripts for tab completion of flags and subcommands.

```bash
# Generate and install completions
baryo completion zsh >> ~/.zshrc
baryo completion bash >> ~/.bashrc
baryo completion fish > ~/.config/fish/completions/baryo.fish
baryo completion powershell >> $PROFILE
```

Supported shells: `zsh`, `bash`, `fish`, `powershell`.

### Worktree isolation

Run Baryo in an isolated git worktree for safe experimentation without affecting your working tree.

```bash
baryo --worktree
```

This creates a new git worktree with a fresh branch based on HEAD. All file operations happen in the worktree. On exit:
- If the worktree has uncommitted changes, the branch name is printed for manual merging
- If no changes were made, the worktree is cleaned up automatically

### Sandboxed code execution

Run generated code in Docker containers for safety — network-isolated, memory-limited, and read-only mounted.

```bash
baryo --sandbox
```

Or enable permanently:

```yaml
# ~/.baryo/config.yaml
sandbox: true
```

When enabled, `run_code` and `run_script` tool calls execute inside Docker containers instead of directly on the host. Each language maps to an appropriate image (`python:3-slim`, `node:20-slim`, `golang:1.22-alpine`, etc.). Containers have no network access, 512MB memory limit, and a 30-second timeout.

### Notifications

Get desktop notifications when the model finishes a response. Useful for long-running tasks.

```yaml
# ~/.baryo/config.yaml
notifications: true
```

Or via environment variable: `BARYO_NOTIFICATIONS=true`. Uses native notification systems — `osascript` on macOS, `notify-send` on Linux. Falls back to a terminal bell character.

## How it works

Baryo communicates with Docker Model Runner through its Unix socket (or TCP endpoint), using the OpenAI-compatible `/v1/chat/completions` API. Responses are streamed token-by-token using Server-Sent Events (SSE).

Models are discovered via `docker model list --json` and the full conversation context is maintained across turns. When tools are provided, the model can make function calls that Baryo executes locally and feeds back into the conversation.

## Architecture

### SSH tunnel auto-launch

Baryo can automatically manage an SSH tunnel to a remote server running Ollama or any OpenAI-compatible API. This is useful when your GPU-powered inference server is on a different machine (e.g. an Oracle Cloud instance).

**How it works:**

1. On startup, Baryo checks if an SSH tunnel is configured (via `--tunnel` flag or `ssh_tunnel` in YAML config)
2. If the local port is already open (manual tunnel), it skips spawning and connects directly
3. Otherwise, it spawns `ssh -N -L local:localhost:remote user@host` as a child process
4. Waits up to 15 seconds for the forwarded port to become reachable
5. Overrides the socket path to `tcp://localhost:<local_port>` so all API calls route through the tunnel
6. On exit (Ctrl+C or normal quit), the SSH process is terminated (SIGINT, then force-kill after 2s)

**For TCP/remote connections, the doctor checks adapt automatically:**

- Skips Docker installed/running/Model Runner checks (irrelevant for remote)
- Runs a `net.DialTimeout` connectivity check against the TCP endpoint instead
- `resolveModel()` constructs the model directly from the `--model` flag (can't run `docker model list` on a remote server)

**Files involved:**

| File | Role |
|------|------|
| `internal/tunnel/tunnel.go` | SSH tunnel lifecycle — `Config`, `Start()`, `Close()`, port helpers |
| `internal/config/config.go` | `SSHTunnel` field on `Config`, YAML parsing, `--tunnel` flag parsing |
| `internal/doctor/doctor.go` | TCP-aware diagnostics (`runTCPChecks` vs `runLocalChecks`) |
| `main.go` | Tunnel startup/teardown, TCP-aware `resolveModel()` |

**Dynamic setup (no config file needed):**

```bash
# One-liner — connect to remote Ollama
baryo --tunnel user@your.server.ip --model qwen3-coder:latest

# Custom SSH port
baryo --tunnel user@myserver.com:2222 --model llama3.2

# Print mode with tunnel
cat main.go | baryo --tunnel user@your.server.ip --model qwen3-coder:latest -p "review this"

# Diagnostics on a remote endpoint
baryo --tunnel user@your.server.ip doctor
```

**Persistent setup (YAML config):**

```yaml
# ~/.baryo/config.yaml
model: "qwen3-coder:latest"

ssh_tunnel:
  host: "your.server.ip"
  user: "your-user"
  remote_port: 11434
  local_port: 11434
  # ssh_port: 22
```

## License

[Mozilla Public License 2.0](LICENSE)
