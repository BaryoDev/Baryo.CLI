Summarize the following conversation between a user and an assistant. Your summary will replace the older messages to free context space while preserving continuity.

## What to Preserve (Critical)

- **Project context**: languages, frameworks, directory structure, build system, dependencies
- **Key decisions**: what was decided and WHY — the rationale matters as much as the decision
- **Specific artifacts**: file paths, function names, class names, variable names, API endpoints, database schemas discussed
- **Current task state**: what was being worked on, what's done, what's pending, what's blocked
- **Code changes**: files created, modified, or deleted — include the purpose of each change
- **Open questions**: unresolved issues, things the user said they'd come back to
- **User preferences**: instructions like "always use X", "don't do Y", tool preferences, coding style preferences
- **Error context**: if debugging was in progress, preserve the error message, what was tried, and what was ruled out
- **Configuration**: any environment variables, config values, API keys (redacted), or setup steps discussed

## What to Drop

- Greetings, pleasantries, and filler ("sure!", "great question", "let me help")
- Repeated explanations of the same concept
- Failed approaches that were fully abandoned (unless the failure itself is instructive)
- Tool call details (the fact that grep was used is less important than what was found)
- Verbose code listings — summarize what the code does rather than reproducing it, unless the exact code is needed for ongoing work

## Format

Write a concise but complete summary in **direct prose**. Do not use conversational framing like "The user asked..." — state facts directly:

Good: "The project uses Go 1.22 with Bubble Tea for the TUI. The search package was refactored to support multiple providers."
Bad: "The user asked about the project structure and the assistant explained that it uses Go."

Use bullet points for lists of files, decisions, or action items. Use code formatting for paths and identifiers.

Aim for 30-50%% of the original conversation length. Err on the side of keeping too much rather than too little — lost context cannot be recovered.

<conversation>
%s
</conversation>