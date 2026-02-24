# Baryo v0.2.0 — Phase 2 is Done

**Your local AI assistant just got a whole lot smarter.**

Baryo started as a simple chat CLI for Docker Model Runner. Phase 1 gave you the basics — model picking, streaming chat, session persistence, markdown rendering, and context management. Phase 2 turns Baryo into something you'd actually want to use every day.

Here's everything new.

---

## Tools That Actually Work

Baryo now gives your local model access to your project. It can read files, search code, explore directories, and check git state — all triggered automatically when the model decides it needs to look something up.

Available tools: `read_file`, `glob`, `grep`, `list_directory`, `git_status`, `git_diff`, `git_log`, and `gh` for GitHub CLI queries. Models with native tool-calling use it directly; for others, Baryo includes a text-based fallback parser that works transparently.

## @Mentions — Attach Files Inline

Type `@` and start typing a filename. Tab completion kicks in immediately, searching recursively across your project. Select a file and its contents are injected as context for the model. Multiple files in one message, deduplication, gitignore-aware, no fuss.

```
explain the relationship between @main.go and @internal/tui/chat.go
```

## Deep Web Search

This is the big one. `/search` no longer just returns a list of links with generic snippets. It now:

1. Searches DuckDuckGo (no API key needed)
2. Fetches the top result pages for real content
3. Extracts article text while stripping CSS, scripts, and junk
4. Sends everything to the model
5. Returns a clean, sourced summary paragraph

The model cites specific article URLs (not just homepages) and asks if you want to dig deeper. No copy-pasting URLs, no manual `/fetch`. One command, real answers.

### Honest Mode

We also taught the model some humility. Instead of hallucinating answers to questions it can't know (current events, real-time data), it now says:

> "I don't have current information about that. Would you like me to search for it?"

Say "yes" and the search triggers automatically. No need to retype anything.

## Project Instructions

Drop a `BARYO.md` in your project root and the model reads it every session. Use `/init` to auto-generate one — Baryo scans your project files, directory structure, config files, and recent commits, then writes tailored instructions.

Also supports `skills.md` for reusable prompt snippets, at both project and user level.

## SSH Tunnels to Remote Models

Got a GPU server running Ollama somewhere? One flag:

```bash
baryo --tunnel opc@your-server --model qwen3-coder:latest
```

Baryo spawns the SSH tunnel, waits for it to connect, routes all API calls through it, and tears it down on exit. Also works via YAML config for persistent setups.

## The Little Things

- **Scroll actually works now** — up/down arrows scroll the conversation without jumping back to the bottom
- **Context-aware compaction** — when token usage gets high, older messages are automatically summarized to free space
- **Conversation export** — `/export` to markdown or JSON, `/copy` for clipboard
- **Fun spinner** — the status bar gets increasingly unhinged the longer the model thinks. Starts professional ("thinking..."), moves to dev excuses ("it worked on my machine", "the AI is hallucinating again", "can this be an email instead?"), escalates to awkward presenter ("fun fact: octopuses have three hearts"), and eventually reaches full meltdown ("this is fine. everything is fine."). Colors cycle too.

---

## What's Next — Phase 3

The [roadmap](../ROADMAP.md) is public. Top of the list:

- **Memory** — persistent context across sessions. `/remember` to save preferences and project facts, auto-injected every session.
- **RAG** — automatic retrieval from project files without manual @mentions
- **Multi-modal input** — image attachments for vision models
- **Plugins** — custom tools via config

---

## Get It

```bash
# Homebrew
brew tap BaryoDev/Baryo.CLI https://github.com/BaryoDev/Baryo.CLI
brew install baryo

# Or build from source
git clone https://github.com/BaryoDev/Baryo.CLI.git
cd Baryo.CLI && go build -o baryo .
```

```bash
# Tag
git tag v0.2.0
git push origin main --tags
```

Baryo is open source under the Mozilla Public License 2.0. Stars, issues, and PRs welcome.

**GitHub:** [github.com/BaryoDev/Baryo.CLI](https://github.com/BaryoDev/Baryo.CLI)
