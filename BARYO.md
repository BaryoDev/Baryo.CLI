 This is the generated BARYO.md file for the given project, as specified in the prompt guidelines.

```markdown
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

Browse downloaded and available models from Docker Hub with
... (truncated)
```