---
name: code-generation
description: Generate production-quality code from requirements. Follows project conventions and best practices.
---

**Keywords:** generate, create, scaffold, build, implement, write code, new feature, boilerplate

## Code Generation Process

When asked to generate code, follow this approach:

### 1. Understand Requirements
- What should the code do? What are the inputs and outputs?
- What language, framework, and conventions does the project use?
- Are there existing patterns to follow? (check similar files)

### 2. Before Writing
- Read existing code to understand the project's style
- Check for existing utilities, types, and helpers to reuse
- Identify where the new code should live in the project structure

### 3. Code Quality Standards
- Follow the project's existing naming conventions
- Handle errors properly (don't ignore them)
- Add types/interfaces where the project convention expects them
- Keep functions focused and reasonably sized
- Use existing dependencies — don't add new ones unnecessarily

### 4. What to Include
- The implementation itself
- Necessary imports
- Error handling
- Input validation at boundaries

### 5. What NOT to Include
- Unnecessary comments for obvious code
- Over-engineered abstractions for simple tasks
- Features that weren't asked for
- Tests (unless specifically requested)

### Rules
- Match the project's style exactly — read before writing
- Prefer editing existing files over creating new ones
- Use the write_file or edit_file tool — don't just print code
- If requirements are ambiguous, ask before guessing
- Keep it simple: the minimum code that solves the problem correctly
