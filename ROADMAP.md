# Baryo Roadmap

## Planned Features

### Memory (Persistent Context Across Sessions)
Allow the model to remember things across chat sessions — user preferences, project decisions, learned facts, and recurring instructions.

**Concept:**
- `/remember <fact>` — explicitly save something (e.g., "always use TypeScript", "project uses PostgreSQL")
- `/forget <fact>` — remove a saved memory
- `/memories` — list all saved memories
- Auto-detect memorable moments — when the user corrects the model or states a preference, offer to remember it
- Memories are stored in `~/.baryo/memories.json` (global) and `.baryo/memories.json` (per-project)
- Injected into the system prompt at the start of each session
- Per-project memories take priority over global ones

**Use cases:**
- "Remember that I prefer concise answers"
- "Remember this project uses Go 1.25 with Bubble Tea"
- "Remember the API base URL is localhost:8080"
- Model stops repeating mistakes the user already corrected

### RAG (Retrieval-Augmented Generation)
Index project files and retrieve relevant context automatically when the user asks questions, instead of requiring manual `@mentions`.

### Multi-Modal Input
Support image attachments in chat for models that support vision.

### Plugin System
Allow users to add custom tools via a plugin config (e.g., run linters, deploy scripts, query databases).

### Conversation Branching
Fork a conversation at any point to explore different directions without losing the original thread.

## Completed

### Deep Web Search (v0.x)
- `/search` auto-fetches top result pages and summarizes with source citations
- Model suggests searching instead of hallucinating
- Auto-triggers search on user agreement ("yes", "sure")
- Context compaction after summary to save tokens

### @ Mentions (v0.x)
- `@filepath` with live tab completion and recursive search
- File contents injected as context

### SSH Tunnel (v0.x)
- Auto-launch SSH tunnels to remote Ollama servers
- `--tunnel user@host` flag and YAML config

### Project Instructions (v0.x)
- `BARYO.md` and `skills.md` for per-project model customization
- `/init` auto-generates project instructions
