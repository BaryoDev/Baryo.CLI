# Baryo

A local AI chat CLI powered by [Docker Model Runner](https://docs.docker.com/desktop/features/model-runner/). Chat with AI models running entirely on your machine — no API keys, no cloud, no data leaving your laptop.

Baryo provides both an interactive terminal UI and a scriptable print mode for pipelines and automation.

## Prerequisites

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) with Model Runner enabled
- At least one AI model pulled:
  ```bash
  docker model pull ai/gemma3
  ```
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
go install github.com/arnelirobles/baryo-cli@latest
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
- A model picker with parameter and size info
- A streaming chat interface with personality — the status bar cycles through fun phrases while the model thinks (dev excuses, awkward presenter moments, and more)
- Input history — press `↑`/`↓` to cycle through previous messages
- Keyboard navigation (`enter` to send, `↑`/`↓` scroll, `ctrl+p`/`ctrl+n` history, `ctrl+c` to quit)

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
- `/resume` — alias for `/sessions`
- `/clear` — start a fresh conversation

### Slash commands

Baryo includes built-in slash commands for common workflows. Type `/help` to see them all.

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
| `/fetch <url>` | Fetch and display a web page |
| `/skills` | List available skills |
| `/skill <name>` | Activate a skill (loads full instructions into context) |
| `/clear` | Start a fresh conversation |
| `/sessions` | List and pick a saved session |
| `/models` | Browse and switch models |
| `/init` | Generate a BARYO.md for this project |
| `/system [prompt]` | View or change the system prompt |
| `/params [k=v]` | View or change model parameters |
| `/context` | Show token usage breakdown |
| `/compact` | Summarize older messages to free context |
| `/export [file]` | Export conversation to a file |
| `/copy` | Copy last response to clipboard |
| `/markdown` | Toggle markdown rendering |
| `/doctor` | Run diagnostic checks |

**Workflows:**

```bash
# Review → Fix → Commit
/review              # Find issues in your changes
# ... fix the issues ...
/commit              # Generate a commit message and commit

# Run tests and check output
/run go test ./...

# Quick question without tool overhead
/ask explain what a goroutine is
```

### Model browser

Browse downloaded and available models from Docker Hub without leaving the TUI.

```bash
# Inside the TUI
/models
```

The model browser shows:
- **Downloaded** models with size/memory info and an `[installed]` tag
- **Available** models from Docker Hub with an `[available]` tag
- Select a downloaded model to start chatting with it
- Select an available model to pull it with live progress

### Startup diagnostics

Baryo checks your Docker setup on every launch. If something is missing, it tells you exactly what's wrong and how to fix it.

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

If a check fails, you'll see what passed and step-by-step instructions to fix the issue. You can also run `/doctor` inside the TUI to check diagnostics mid-session.

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
enter send • ↑↓ scroll • ctrl+p/n history • ctrl+c quit          ~3.2k / 8k
```

The token count is color-coded: dim when under 60%, amber at 60-85%, and red above 85%.

**Commands:**

| Command | Description |
|---------|-------------|
| `/context` | Show a detailed breakdown of token usage (system prompt, conversation, total) |
| `/compact` | Summarize older messages to free context space, keeping the last 4 exchanges |

**Auto-compaction** triggers automatically when token usage exceeds 85% of the context limit and there are enough messages to compact. Older messages are replaced with a summary while recent exchanges are kept verbatim.

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
| `--skip-checks` | Skip startup health checks |
| `--version` | Print version and exit |
| `--help` | Print usage and exit |
| `doctor` | Run full diagnostic check (subcommand) |

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
| `DOCKER_MODEL_SOCKET` | Docker Model Runner socket path (legacy) |

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

# Recursive matching — typing @cha finds internal/tui/chat.go
@cha               # shows matches across all subdirectories
```

**How it works:**

- As you type after `@`, matching files appear in the status bar
- **Tab** / **Shift+Tab** — cycle through candidates
- **Enter** — select the highlighted file (inserts it into your input)
- **Esc** — dismiss suggestions
- Press **Enter** again to send — file contents are injected as context for the model

Files must be under 100KB, non-binary, and not gitignored. Directories can be browsed via tab completion but not attached directly. Duplicate mentions are deduplicated.

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

The model is instructed to be honest about uncertainty. If you ask something it doesn't know, it will offer to search instead of guessing:

```
You: What happened in the Senate hearing today?
Assistant: I don't have current information about that. Would you like me to search for it?
You: yes
→ automatically triggers a search using your original question
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

### Project instructions

Baryo loads project-specific instructions from `BARYO.md` files and injects them into the system prompt. This lets you customize the model's behavior per-project.

Baryo checks these locations (all are optional, all found are combined):

| File | Scope |
|------|-------|
| `BARYO.md` | Project root — project-specific instructions |
| `.baryo/BARYO.md` | Project config directory — alternative location |
| `~/.baryo/BARYO.md` | User home — global instructions for all projects |

Use the `/init` command inside the TUI to generate a `BARYO.md`. The model reads your project files (README, config files, directory structure, recent commits) and writes tailored instructions automatically.

### Skills (Agent Skills format)

Baryo supports the [Anthropic Agent Skills](https://github.com/anthropics/skills) format. Skills are directories containing a `SKILL.md` file with YAML frontmatter and markdown instructions, plus optional `scripts/` and `resources/` directories.

```
my-skill/
├── SKILL.md          # Required — YAML frontmatter + instructions
├── scripts/          # Optional — executable scripts
└── resources/        # Optional — supporting files
```

**SKILL.md format:**

```yaml
---
name: my-skill
description: What this skill does and when to use it
---

# My Skill

Instructions the model follows when this skill is active.
```

**Skill directories are scanned from:**

| Path | Scope |
|------|-------|
| `~/.baryo/skills/*/SKILL.md` | Global skills for all projects |
| `.baryo/skills/*/SKILL.md` | Project config directory |
| `skills/*/SKILL.md` | Project root |

If a skill includes a `scripts/` directory, the model is told about available scripts and can suggest running them via `/run`. Project-level skills override global skills with the same name.

Baryo ships with skills from the [Anthropic Agent Skills](https://github.com/anthropics/skills) repository in the `skills/` directory. Skills are **lazy-loaded** — only names and descriptions are read on startup. Full content is loaded on-demand when you activate a skill.

```bash
# List available skills
/skills

# Activate a skill — loads full instructions + scripts into context
/skill pdf
/skill internal-comms

# Then just ask naturally
create a sample incident report for the outage last night
```

The model sees the skill index in the system prompt and can suggest activating skills when relevant (e.g., "You might want to run `/skill pdf` for this task").

**Install skills globally:**

```bash
cp -r skills/pdf ~/.baryo/skills/pdf
```

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
