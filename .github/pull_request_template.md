<!-- Title format: area: what changed (closes #123)
     e.g. "index: stop dropping symbols in pure-Go builds (closes #28)" -->

<!-- Conventions live in BARYO.md and CONTRIBUTING.md. Most review comments on this
     repository are already written down in one of them. -->

## What changed and why

<!-- What was wrong, and what this does about it. Link the issue. -->

Fixes #

## How to test

<!-- The commands a reviewer runs to see this working. -->

## Which test fails without this change

<!-- Name it. For a bug fix, a test that passes both with and without the change proves
     nothing: write it first and watch it fail, or revert the fix and confirm it goes red.
     For docs, build or dependency changes, write "not applicable". -->

## Checklist

- [ ] `sh scripts/ci-local.sh` passes
- [ ] A test covers this, and I confirmed it fails without the change
- [ ] Tested under **both** `CGO_ENABLED=0` and `CGO_ENABLED=1` if it touches `internal/index`
- [ ] `NOTICE` regenerated if dependencies changed (`python3 scripts/gen_notice.py > NOTICE`)
- [ ] Docs updated if behaviour, flags or configuration changed

## Anything reviewers should know

<!-- Trade-offs, things deliberately left out, measurements, follow-up issues filed.
     If this changes performance or binary size, put the numbers here. Leave blank if
     there is nothing. -->
