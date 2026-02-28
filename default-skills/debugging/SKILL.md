---
name: debugging
description: Debug issues systematically. Reproduce, hypothesize, isolate, fix, and verify.
---

**Keywords:** debug, fix error, not working, broken, crash, exception, stack trace, troubleshoot, diagnose

## Debugging Process

When asked to debug an issue, follow this systematic approach:

### 1. Reproduce
- Get the exact error message, stack trace, or unexpected behavior
- Identify the steps to reproduce
- Determine: does it always happen, or intermittently?

### 2. Hypothesize
- Based on the error, form 2-3 hypotheses about the root cause
- Rank them by likelihood
- Consider recent changes that might have introduced the bug

### 3. Isolate
- Read the relevant code paths
- Trace the execution flow from input to error
- Narrow down to the specific function/line causing the issue
- Check: inputs, state, dependencies, environment

### 4. Fix
- Address the root cause, not just the symptom
- Make the minimal change needed
- Consider edge cases the fix should handle
- Don't introduce new issues

### 5. Verify
- Confirm the fix resolves the original issue
- Check that nothing else broke
- Run existing tests if available

### Common Patterns
- **Null/nil errors** — trace where the value should have been set
- **Off-by-one** — check loop bounds, array indices, string slicing
- **Race conditions** — look for shared mutable state
- **Wrong type** — check type assertions, conversions, JSON parsing
- **Missing error handling** — check every error return
- **Environment issues** — check env vars, file paths, permissions

### Rules
- Read the code before guessing
- Don't shotgun debug (random changes hoping something works)
- Fix the cause, not the symptom
- If you can't reproduce it, gather more information first
- Explain what went wrong and why, so the user learns
