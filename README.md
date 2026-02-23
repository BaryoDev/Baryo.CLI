# Baryo

A local AI chat CLI powered by [Docker Model Runner](https://docs.docker.com/desktop/features/model-runner/). Chat with AI models running entirely on your machine — no API keys, no cloud, no data leaving your laptop.

Baryo provides both an interactive terminal UI and a scriptable print mode for pipelines and automation.

## Prerequisites

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) with Model Runner enabled
- At least one AI model pulled:
  ```bash
  docker model pull ai/mistral
  ```
- [Go 1.25+](https://go.dev/dl/) (to build from source)

## Installation

```bash
# Clone and build
git clone https://github.com/arnelirobles/baryo-cli.git
cd baryo-cli
go build -o baryo .

# Move to your PATH
sudo mv baryo /usr/local/bin/
```

Or install directly with Go:

```bash
go install github.com/arnelirobles/baryo-cli@latest
```

### Build with version info

```bash
go build -ldflags "-X github.com/arnelirobles/baryo-cli/internal/cli.Version=0.1.0" -o baryo .
```

## Usage

### Interactive mode (TUI)

```bash
# Launch the TUI — pick a model, then chat
baryo

# Skip the model picker
baryo --model mistral
```

The interactive mode gives you:
- A model picker with parameter and size info
- A streaming chat interface
- Input history — press `↑`/`↓` to cycle through previous messages
- Keyboard navigation (`enter` to send, `ctrl+c` to quit)

### Print mode

Send a prompt and get the response streamed to stdout. Useful for scripts and pipelines.

```bash
# Ask a question
baryo -p "what is 2+2"

# Use a specific model
baryo --model mistral -p "explain docker networking"

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
| `--skip-checks` | Skip startup health checks |
| `--version` | Print version and exit |
| `--help` | Print usage and exit |
| `doctor` | Run full diagnostic check (subcommand) |

### Model matching

The `--model` flag uses smart matching:

1. **Exact match** — `--model ai/mistral`
2. **Short name** — `--model mistral` (matches `ai/mistral`)
3. **Substring** — `--model mis` (matches `ai/mistral`)

If the query is ambiguous, you'll see the list of matching models. If nothing matches, you'll see all available models.

## Configuration

Baryo supports layered configuration through YAML files and environment variables.

### Config files

Create a YAML config file at either location:

- **User-level:** `~/.baryo/config.yaml`
- **Project-level:** `.baryo/config.yaml` (overrides user config)

```yaml
# ~/.baryo/config.yaml
model: ai/mistral
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
| Linux | `~/.docker/desktop/inference.sock` |

## How it works

Baryo communicates with Docker Model Runner through its Unix socket, using the OpenAI-compatible `/v1/chat/completions` API. Responses are streamed token-by-token using Server-Sent Events (SSE).

Models are discovered via `docker model list --json` and the full conversation context is maintained across turns.

## License

[Mozilla Public License 2.0](LICENSE)
