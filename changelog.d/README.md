# changelog.d

One file per change. `scripts/changelog-assemble.sh` folds them into `CHANGELOG.md` at
release time and deletes them.

## Why

When every pull request adds its entry to `CHANGELOG.md` directly, every open branch
conflicts on that one file after every merge — and the conflict gets resolved by hand at
the point of lowest attention. Nothing compiles Markdown, so a botched resolution passes
every other gate in this pipeline: conflict markers reach the default branch, or an entry
gets pasted back into a file that already holds it.

Two branches adding two files do not conflict. That is the whole idea.

## Adding one

Name the file `<slug>.<section>.md`. The slug is yours — an issue number or a short
description — and the section has to be one of `Breaking`, `Added`, `Changed`, `Removed`,
`Fixed`, `Security`, matching the headings `CHANGELOG.md` already uses.

```
changelog.d/28.Fixed.md
changelog.d/pure-go-parser.Added.md
changelog.d/drop-legacy-flag.Breaking.md
```

Write the entry exactly as it should appear in the changelog: open with a bolded lead
saying what was wrong, then what changed.

```markdown
- **Released binaries shipped an empty repo map.** The tree-sitter parsers were cgo-only
  and releases are built with `CGO_ENABLED=0`, so every file was dropped as unparseable.
  A pure-Go parser now runs in those builds.
```

`scripts/changelog-assemble.sh --check` validates fragments without changing anything, and
CI runs it on every branch. It checks the section is real, the file is not empty, and the
lead is bolded.

## Releasing

Run `bash scripts/changelog-assemble.sh`, read the diff, commit. `scripts/check-release-ready.sh`
refuses to call a release ready while unassembled fragments are still here, so this cannot be
forgotten.
