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

### Flags

| Flag | Description |
|------|-------------|
| `-p <prompt>` | Send a prompt in non-interactive (print) mode |
| `--model <name>` | Select a model by name or substring |
| `--version` | Print version and exit |
| `--help` | Print usage and exit |

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
```

### Environment variables

| Variable | Description |
|----------|-------------|
| `BARYO_MODEL` | Default model |
| `BARYO_SOCKET` | Docker Model Runner socket path |
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
