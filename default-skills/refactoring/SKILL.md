---
name: refactoring
description: Refactor code to improve structure and readability. Uses named patterns and incremental, behavior-preserving transforms.
---

**Keywords:** refactor, clean up, simplify, restructure, improve code, code smell, extract, rename

## Refactoring Process

When asked to refactor code, follow this approach:

### 1. Understand the Current Code
- Read the code thoroughly before changing anything
- Identify what it does and verify you understand the behavior
- Note the test coverage (if any) — tests are your safety net

### 2. Identify Code Smells
- **Long methods** — break into focused functions
- **Duplicate code** — extract shared logic
- **Deep nesting** — use early returns, guard clauses
- **God objects** — split responsibilities
- **Primitive obsession** — introduce domain types
- **Feature envy** — move logic to where the data lives
- **Dead code** — remove unused functions, variables, imports

### 3. Apply Named Patterns
Use well-known refactoring patterns:
- **Extract Method** — pull logic into a named function
- **Extract Variable** — name complex expressions
- **Inline** — remove unnecessary indirection
- **Rename** — improve clarity of names
- **Move** — relocate to a better home
- **Replace Conditional with Polymorphism** — when appropriate
- **Introduce Parameter Object** — for long parameter lists

### 4. Rules
- **Preserve behavior** — refactoring must not change what the code does
- **Incremental steps** — make small, verifiable changes
- **One thing at a time** — don't mix refactoring with feature changes
- **Keep it simple** — the goal is clarity, not cleverness
- **Verify after each step** — run tests if available
- Don't refactor code that works fine just because you can
- Don't add abstractions for single-use code
