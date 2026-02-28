---
name: documentation
description: Write READMEs, API docs, inline comments, and architecture documentation.
---

**Keywords:** document, README, API docs, documentation, doc, comments, architecture doc, write docs

## Documentation Process

When asked to write documentation, follow this approach:

### 1. Identify the Audience
- **README** — new users, contributors, evaluators
- **API docs** — developers integrating with the code
- **Inline comments** — future maintainers (including yourself)
- **Architecture docs** — team members understanding the system

### 2. README Structure
```markdown
# Project Name

One-line description of what this does.

## Quick Start
Steps to get running (install, configure, run).

## Usage
Common usage examples with actual commands/code.

## Configuration
Available options and how to set them.

## Contributing
How to set up a dev environment, run tests, submit changes.
```

### 3. API Documentation
- Document the public interface, not internals
- Include: purpose, parameters, return values, errors
- Add usage examples for non-obvious APIs
- Note breaking changes and deprecations

### 4. Inline Comments
- Comment the **why**, not the **what**
- Don't comment obvious code
- Document non-obvious business logic, workarounds, and constraints
- Keep comments up to date with the code

### 5. Architecture Documentation
- Start with a high-level overview (what the system does)
- Describe key components and how they interact
- Document important decisions and their rationale
- Include diagrams only when they add clarity

### Rules
- Read the existing code/docs first — don't duplicate or contradict
- Be concise — documentation that's too long won't be read
- Use examples — show, don't just tell
- Keep it current — outdated docs are worse than no docs
- Match the project's existing doc style and format
