```markdown
# Baryo CLI

A local AI chat CLI powered by Docker Model Runner with cloud provider support, providing both interactive TUI and scriptable print modes.

## Tech Stack

- **Language**: Go 1.25+
- **Frameworks**: 
  - Charm Bubble Tea (TUI framework)
  - AWS SDK for Bedrock support
  - Tree-sitter for code parsing
- **Build Tools**: GoReleaser for multi-platform distribution
- **Packaging**: Homebrew, Scoop, and manual distribution

## Key Directories

### `/internal/` - Core Application Logic
- `cli/` - Command-line interface implementation
- `config/` - Configuration management and memory handling
- `llm/` - Language model providers (local Docker, cloud APIs)
- `tui/` - Terminal UI components and state management
- `tools/` - File operations, git integration, and external tools
- `rag/` - Retrieval-augmented generation and document handling
- `search/` - Web search and research capabilities

### `/skills/` - Specialized Functionality
205+ pre-built skills for document processing, testing, and creative workflows including PDF, DOCX, PPTX, XLSX handling and web application testing.

### `/cmd/` - Command Executables
Entry points for different binary builds (main application).

### Platform Packaging
- `HomebrewFormula/` - Homebrew tap formula
- `ScoopBucket/` - Scoop package manifest
- `/assets/` - Release assets and build artifacts

## Coding Guidelines

### Style & Conventions
- **Go Standards**: Follow standard Go conventions with gofmt
- **Error Handling**: Explicit error checking with meaningful error messages
- **TUI Patterns**: Use Charm Bubble Tea patterns with clear state management
- **Module Organization**: Internal packages for implementation details

### File Organization
- Keep related functionality in cohesive internal packages
- Use descriptive file names matching their primary responsibility
- Separate concerns: CLI parsing, UI rendering, business logic

### Naming Conventions
- Package names: lowercase, descriptive (e.g., `cli`, `tui`, `llm`)
- Exported types/functions: PascalCase
- Internal implementation: lowercase or mixed case as appropriate

## Build & Development Commands

### Basic Commands
```bash
# Build from source
go build -o baryo .

# Run tests
go test ./...

# Format code
go fmt ./...
```

### Release Commands
```bash
# Build all platform binaries
goreleaser build --snapshot

# Create release (requires tagging)
git tag vX.Y.Z
git push origin main --tags
```

### Package Management
```bash
# Update dependencies
go mod tidy

# Install to GOPATH
go install github.com/baryodev/baryo-cli@latest
```

## Skills Commands

### Core Workflows
- `/review` - Analyze code changes for bugs and style issues
- `/commit` - Generate commit message and commit staged changes
- `/diff` - Show git differences for current changes
- `/test` - Run project tests and validation

### File Operations
- `/edit <file>` - Make targeted edits to source files
- `/create <file>` - Generate new files with appropriate structure
- `/read <file>` - Examine file contents with line numbers

### Project Management
- `/status` - Check git status and current branch
- `/log` - Show recent commit history
- `/doctor` - Run diagnostic checks on the project

### Research & Documentation
- `/search <query>` - Web search with summarized results
- `/research <topic>` - Deep research with structured analysis
- `/docs` - Generate or update documentation

## Skill-Specific Commands

Activate skills for specialized workflows:
- `/skill docx` - Word document processing
- `/skill pdf` - PDF manipulation and analysis
- `/skill pptx` - PowerPoint presentation handling
- `/skill webapp-testing` - Browser automation testing
- `/skill mcp-builder` - Model Context Protocol development

## Project-Specific Patterns

### Model Provider Architecture
- Support both local Docker Model Runner and cloud providers
- Consistent interface abstraction across providers
- Graceful fallback when Docker unavailable

### Skill System
- Auto-activation based on user intent
- Pluggable architecture for extensibility
- Skill-specific tools and workflows

### TUI Design Principles
- Clean, muted interface with minimal visual clutter
- Tabbed navigation for complex feature sets
- Structured tool call visualization
- Quirky, personality-driven status indicators
```