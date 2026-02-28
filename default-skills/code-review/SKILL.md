---
name: code-review
description: Review code for bugs, security issues, performance problems, and style. Produces structured findings by severity.
---

**Keywords:** review, find bugs, security, code review, audit, check code, code quality

## Code Review Process

When asked to review code, follow this structured approach:

### 1. Understand Context
- What does this code do? What is its purpose?
- What language, framework, and patterns are in use?
- Is this a PR diff, a single file, or a whole project?

### 2. Review Categories

Check each category systematically:

**Bugs & Correctness**
- Logic errors, off-by-one, null/nil dereference
- Race conditions, deadlocks
- Missing error handling, unchecked returns
- Edge cases: empty inputs, overflow, unicode

**Security**
- Injection vulnerabilities (SQL, command, XSS)
- Authentication/authorization flaws
- Hardcoded secrets, insecure defaults
- Input validation gaps

**Performance**
- Unnecessary allocations or copies
- N+1 queries, missing indexes
- Unbounded growth (memory leaks, goroutine leaks)
- Inefficient algorithms for the data size

**Style & Maintainability**
- Naming clarity, consistent conventions
- Dead code, unused imports
- Missing or misleading comments
- Overly complex logic that could be simplified

### 3. Output Format

Present findings grouped by severity:

```
## Critical
- [file:line] Description of the issue
  → Suggested fix

## Warning
- [file:line] Description of the issue
  → Suggested fix

## Info
- [file:line] Suggestion or improvement
```

### Rules
- Be specific: always include file and line references
- Prioritize: critical bugs first, style last
- Be actionable: suggest fixes, not just problems
- Be honest: if the code looks good, say so
- Don't nitpick: focus on issues that matter
