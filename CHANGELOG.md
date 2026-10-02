# Changelog

## Unreleased

## v0.14.0 (2026-10-02)

Project config needs trust before it applies, MCP tools pass the permission gate, and released binaries get their repo map back. Upgrade if you run Baryo in repositories you did not write.

### Breaking

- **A project's `.baryo/config.yaml` does nothing until the directory is trusted.** Interactive runs ask once and remember the answer under `~/.baryo/trusted/`. Anything non-interactive (`-p`, `doctor`) is untrusted unless it passes `--trust-project`, which applies to that run and records nothing. A script or CI job that relied on project config needs the flag. The reason is under Security. (#16)
- **`permission_mode: confirm` now prompts for MCP tool calls.** They used to run with no prompt. A tool the server annotates with `readOnlyHint`, or a server marked `trust: read-only` in config, is not gated. (#16)

### Added

- **A trace of what a task did.** The saved conversation kept the narration only: tool calls, their arguments and their results were run and thrown away. `internal/trace` appends them as JSON lines to `~/.baryo/sessions/<id>.trace.jsonl`, beside the session, with `task_start`, `tool_call`, `tool_result`, `diff`, `verify` and `task_end` records. Known provider keys and common token shapes are masked before writing. `trace: false` turns it off, and `--trace-file` names the file in print mode. (#22)
- **`--timeout` gives print mode an overall deadline,** and `stream_idle_timeout` (default 5m) sets how long a silent stream is waited on. (#18)
- **`baryo doctor` reports whether symbol extraction is available.** (#21)
- **`install.sh` verifies the download against the release's `checksums.txt`.** This catches a truncated or swapped archive. It is not a signature. (#14)
- **`cmd/baryo-runner`, the three-arm benchmark runner for phase 1.** It runs each task with the local model alone, the local model with a recipe, and a cloud model, each in its own worktree, and reports whether the recipe arm beats the bare local arm by 25 points. Built from source, not in the release archives. (#48, by @teddyvj)

- **`baryo export` writes session history out.** `--format baryo-jsonl` is built in: one self-describing JSONL file with a manifest record, a record per session, and one event per message — archived messages first, then the live conversation. Tool calls and their results are preserved and labelled, since those are exactly what a summary loses. `--since`, `--session` and `--out` scope it.
- **Plugins.** A plugin is a directory with a `plugin.yaml` and an executable, run as a subprocess speaking JSON over stdin and stdout. `baryo plugins` lists what is installed, `baryo plugins inspect <name>` shows what it can do and what it asks for before you trust it. `exporter` is the first capability kind; `tool`, `hook`, `skill` and `provider` are reserved and refuse to load with a clear message.
- **A plugin can only execute a file it ships.** An absolute command, a path climbing out of the plugin directory, or a symlink resolving outside it is refused, so reviewing a plugin means reviewing one directory. Project plugins (`.baryo/plugins/`) additionally require `--trust-project`, the same gate that already covers a project's config, skills and hooks — and a project plugin cannot take a global plugin's name.
- **Archived messages carry a timestamp and a sequence number.** `llm.ChatMessage` has neither, so an archive line could not say when its message happened, which an exporter needs and ctx's history format requires per event. The sequence is counted from the file so that resuming a session in a new process does not restart it. Archives written before this keep working, report the zero time rather than an invented one, and are flagged in an export warning. `session.LoadArchiveRecords` exposes the metadata; `LoadArchive` is unchanged.

- **`scripts/ci-local.sh` runs the gates CI runs.** Every contribution arrives from a fork, and a fork's pull request cannot see anything not already merged here, so finding out that gofmt objects used to cost a push and a review round trip. One command now gives the same answer locally, keeps going after a failure, and prints the full list at the end.
- **CI runs on every branch except `main`.** A contributor working in a fork gets the full pipeline on their own pushes instead of first learning what CI thinks after opening a pull request. `main` is covered by the pull request that merges into it, by the release workflow that calls this one, and by the weekly schedule.
- **Secret scan (gitleaks) over full history**, with a step that plants a credential and fails the build if the scan does not catch it. The scanner is the checksum-verified MIT binary rather than the licensed Action, which reports success without a licence and works the same on a fork, where no repository secret is available.
- **Conflict marker gate**, plus a step proving it can fail. Nothing compiles Markdown, so a CHANGELOG committed mid-merge passed every other gate.
- **CodeQL** for Go and for the workflows themselves, on `security-extended`.
- **Dependabot** for Go modules and for GitHub Actions. Actions are pinned by commit SHA here, which is correct and also means a pin goes stale silently.
- `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`, `CODEOWNERS`, a pull request template, and issue forms.
- Design: [plugin architecture](design/specs/2026-09-13-baryo-plugin-architecture.md) — five extension points, plugins as subprocesses rather than Go `plugin` builds, project plugins behind the existing `--trust-project` gate, and history export to ctx as the first case.

- **Changelog entries are fragments in `changelog.d/`.** One file per change, folded into `CHANGELOG.md` at release time by `scripts/changelog-assemble.sh`. Editing `CHANGELOG.md` in every pull request makes every open branch conflict on one file, and nothing compiles Markdown, so a botched resolution passes every other gate. CI validates fragments and proves the assembler files them under Unreleased rather than under the release that last shipped.
- **`scripts/check-release-ready.sh` says whether a release can ship.** It refuses while fragments are unassembled, while `CHANGELOG.md` has no section for the version, while entries are still under Unreleased, or when the tag already exists. Runs on demand and weekly via the `release-ready` workflow, and gates the release workflow itself before anything is published.
- **Releases publish an SBOM per archive.** Generated by a pinned, checksum-verified syft from the built archive rather than from `go.mod`, because the question worth answering is what ended up in the artifact. CI generates one the same way and fails if it lists no components, since a tool that writes an empty file still exits 0 — and the release fails if no SBOM was attached.
- **CI checks that every workflow `run:` block is valid shell.** A run block is a script nothing parses until the job executes it, so an unbalanced quote is a perfectly good YAML string that reaches the runner and fails the job minutes in, after setup and every step ahead of it. `scripts/check-workflow-shell.sh` parses all 28 of them with `bash -n` in about a second. It exists because exactly that typo shipped in the step that proves the conflict marker check can fail.

### Changed

- **Relicensed from MPL-2.0 to MIT.** `NOTICE` is generated by `scripts/gen_notice.py` and lists every module that ships in the binary with its licence text. It named 4 of 54 before. CI fails when it is stale, and the generator fails if a dependency is GPL, LGPL, AGPL or MPL. (#19)
- **The rewrite pass no longer runs against local endpoints.** It cost a full request before the real one, often ran past its own 15s timeout on CPU, and on a single-slot server it evicted the conversation's KV cache. (#15)
- **Release binaries are larger.** The pure-Go parsers bundle grammars for 8 languages: about 21 MB became about 46 MB, as measured in #47.
- **CI runs before a release is published,** and weekly, so `govulncheck` sees advisories published after the last push. A tag used to publish whatever was on it with no test step. (#14)
- **The Go toolchain is 1.26.6.** (#3)
- `endpointForModel` moved from the TUI into `internal/llm` with table tests, and its three branches are documented. (#44, #46)

### Removed

- **Third-party skills are gone from the repository.** 15 of the 17 directories under `skills/` carried a licence that forbids redistribution. They were never embedded in the binary or shipped in a release archive. Install them from their source into `~/.baryo/skills`. (#13)
- **The `go install` instructions are gone from the README.** They named a module path that does not exist. (#14)

### Fixed

- **Released binaries shipped an empty repo map.** Releases build with `CGO_ENABLED=0`, the parser stub for that build returned an error for every file, and the index dropped each file it got an error for. The model had no view of the project at all. Files are now kept (#21), and symbol extraction works in release builds through a pure-Go tree-sitter runtime, for Go, JavaScript, TypeScript, Python, Rust, Java, C and C++ (#47, by @teddyvj).
- **A project inside a dotted directory had an empty index.** The walk skipped any directory whose name starts with a dot, including the root, so working in `~/.dotfiles` or `~/.config/nvim` gave an empty repo map with no error. (#20)
- **A provider that sent headers and then went silent hung forever.** Only the header timeout was set. An idle watchdog now cancels the request on the OpenAI-compatible, Anthropic and Bedrock streaming paths. (#18)
- **A timed-out print-mode run exited 0.** A CI job that hit its deadline reported success. Both the text and JSON exits now return 1. (#18)
- **The prompt prefix changed on almost every request.** The tools array was built by iterating a map, so its order moved between calls and a local server could not reuse its KV cache: the tool schemas and the whole conversation were prefilled again each turn. Tools, MCP servers and pinned files are now in a fixed order. (#15)
- **Every print-mode run with no MCP servers panicked on main after #15.** A typed nil pointer was stored in an interface, so the nil check passed. This never reached a release. (#22)
- **Ignore checks ran one `git check-ignore` process per file.** grep, glob, `list_directory`, the repo index and the RAG store each did it, and the index walk runs after every turn. They are batched into one process: 892ms became 14ms on this repo. (#20)
- **@mention completion never filtered ignored files.** It passed newline-joined paths to `git check-ignore -z`, which git reads as one path name. (#20)
- **`install.sh` failed after installing on the sudo path,** because `chmod +x` ran on a file root already owned. A truncated `curl | sh` could also run half the script, which is now wrapped in `main`. (#14)

- **Four paths that dropped conversation history now archive it.** Compaction archived the messages it replaced and was the only path that did: `/clear` discarded the whole conversation, and the search, research and strategy compactions each shrank a bulky message in place. In all four cases the original was gone, which is the content a later `/sessions search` — or an export — wants back.
- **`NOTICE` is generated from both cgo settings, not just the local one.** `go list -deps` answers for one build configuration, and `internal/index` has real dependencies behind `//go:build` tags. A machine with a C toolchain defaults to cgo on, so the generator attributed the cgo build and silently omitted whatever only the `CGO_ENABLED=0` build links — which is the build `goreleaser` publishes. Neither the generator nor the CI check could see the gap, because both agreed with each other.
- `govulncheck` is pinned (`v1.8.0`) like every other tool in the pipeline. The pin has a floor as well as a ceiling: govulncheck analyses with the toolchain it was built with, so a version older than this module's `go` directive refuses every package.

- **CodeQL's Go analysis no longer depends on guessing a build.** `build-mode: autobuild` failed after extracting 97 of 142 files, because `internal/index` has two parser implementations behind build tags. Go went buildless first, then CodeQL stopped accepting `build-mode: none` for Go on 13 September and every run died before a query ran. It is `build-mode: manual` with an explicit `go build ./...` now. (#53)
- **The exported history no longer publishes an archive timestamp as `occurred_at`.** Nothing in Baryo records when a message was sent, so the only time available is when compaction archived it — an upper bound. A field named `occurred_at` invites an importer to read that as the moment the message happened, which is the same mistake as inventing a timestamp. Events now carry `archived_at` where it is known, plus a `time_fidelity` of `archived_upper_bound` or `unknown`, and never `occurred_at`.
- **The conflict marker gate skipped filenames containing spaces.** It looped over unquoted `git ls-files`, so a tracked file named `release notes.md` split into two non-existent paths and was never read — the one file the gate existed to check. It uses `git grep` now, which takes filenames from git rather than through the shell, and the step that proves the gate can fail uses a filename with a space.
- **`scripts/ci-local.sh` reported a failure where it promised a skip.** The race detector is built on cgo and needs a C compiler, but only the cgo test pass was guarded. `--help` also printed two lines of the script's own code.
- **`scripts/check-notice.sh` wrote to a predictable path in `/tmp`.** A shared name there can be pre-created as a symlink by another local user, and two concurrent runs overwrote each other. It uses `mktemp` and removes the file on exit.
- **The release config used properties goreleaser has deprecated.** `archives.format` and `format_overrides.format` are replaced by their plural forms. Nothing exercised the release config except a tag, which is the worst moment to discover it; `goreleaser check` now runs in CI.
- **`NOTICE` omitted two dependencies that ship in the Windows binaries.** Attribution was generated for one build target at a time, and bubbletea pulls `erikgeiser/coninput` and `mattn/go-localereader` on Windows only — so a `NOTICE` generated on Linux left both out of every release. The generator now resolves all six published GOOS/GOARCH targets plus the host's cgo build and merges them.

### Security

- **A cloned repository's config ran its code.** `./.baryo/config.yaml` was merged with no gate, and it can set `hooks` (run through `sh -c` on the first tool call), `mcp_servers` (started as processes), `ssh_tunnel`, `permission_mode` and `system_prompt`. An untrusted project's config is now ignored in full. Project `skills/` and `.baryo/skills` are `run_script` roots only when trusted. `BARYO.md` still loads, wrapped in a block that tells the model it is information and not authority, and it is skipped for an untrusted project in `auto` mode. (#16, closes #12)
- **MCP tools skipped the permission gate.** Every executor sent MCP calls to the server before any permission check, so a third-party write or exec tool ran unprompted in `confirm` mode, and in the plan, ask, architect and review modes that call themselves read-only. (#16)
- **An unrecognised `permission_mode` was treated as `auto`.** The value is checked when config loads, and an unknown one falls back to `confirm` with a warning. (#16)
- **The permission gate answered "safe" for tool names it did not know.** Unknown names now count as destructive and are rejected before the gate. (#16)
- **A symlink inside a skill directory let `run_script` run a file outside it.** Both the script path and the skill root are resolved before the check. (#18, closes #8)
- **19 advisories in the Go standard library and `golang.org/x/text` are patched,** by moving the toolchain to 1.26.6 and `x/text` to 0.39.0. (#3)

## v0.13.0 — Lossless compaction (2026-07-13)

Context compaction no longer destroys history, and a failed compaction can no longer corrupt the conversation.

### Added

- **Pre-compaction messages are archived to disk.** When compaction replaces older messages with a summary, the replaced messages are appended to `~/.baryo/sessions/<id>.archive.jsonl` (one JSON message per line). Previously the saved session stored only the compacted list, so summarized-away content was permanently lost.
- **`/sessions search` now covers archived messages.** Content that was compacted out of the live conversation is searchable again; before, search silently only worked on conversations short enough to never compact.
- `session.LoadArchive(id)` reads a session's full archived history (groundwork for a future `/recall` command).
- Session retention cleanup (`session_retention_days`) removes a session's archive together with its session file.

### Fixed

- **A failed compaction stream no longer corrupts the next turn.** If the compaction request errored (model busy, endpoint drop), the pending-compaction flag was never cleared, so the model's next normal reply was spliced into history as if it were the summary — silently destroying older messages with a stale keep index. The error path also popped a legitimate user message off the conversation. Compaction failures now report "context unchanged" and leave the conversation intact.
- Stream-state reset clears compaction bookkeeping on every path (error, cancel), not just success.

### Tests

- First tests for the `session` package: archive round-trip, search-over-archive, and archive cleanup.

## v0.12.1 — Stabilization release (2026-07-10)

No new features. This release hardens v0.12.0 for daily use: crash fixes, data-loss protection, correctness fixes in the tool loop, dependency security updates, and a stricter CI gate.

### Fixed — crashes and data loss

- **`apply_diff` no longer crashes the app** on malformed model-generated diffs. Two panic paths fixed: a pure-insertion hunk targeting a line past the end of the file, and a bare `@@` hunk header.
- **File writes are now atomic** (write to temp file, then rename). `edit_file`, `write_file`, `apply_diff`, session saves, and memory saves can no longer truncate or corrupt a file if the process is killed mid-write. Previously a crash during a session save could destroy the entire conversation history.
- **Subprocess output is capped in memory.** `shell`, `run_script`, `run_code`, git/gh tools, and the Docker sandbox previously buffered unbounded output before truncating; a runaway command (`yes`, huge build logs) could exhaust RAM before the timeout fired.

### Fixed — wrong behavior

- **Repeat tool calls with different arguments are no longer blocked.** The duplicate-call detector keyed most tools on a constant, so the second `read_file`, `grep`, or `shell` call in a turn was rejected with "Already retrieved results for this query", forcing the model to guess. Deduplication now compares full arguments; only the search-family tools keep the shared query namespace.
- **No more phantom "Model returned an empty response" errors after confirming a tool** in `confirm` permission mode. Approving/denying a destructive tool registered a duplicate event listener, which double-processed the end of the stream.
- **Truncated model streams now surface an error** instead of silently ending mid-answer. Both stream readers check for read errors, and the OpenAI-compatible path got the same 1 MB line buffer as the Anthropic path (large tool-call arguments could exceed the old 64 KB limit).
- **Dead MCP servers fail fast.** If an MCP server crashes or sends an oversized message, calls now return an immediate error naming the server, instead of every subsequent tool call hanging for 30 seconds forever.

### Fixed — hardening

- **Symlinks can no longer escape the project directory.** The path guard used by `edit_file`, `write_file`, `delete_file`, and `apply_diff` now resolves symlinks before checking containment.
- **Process hygiene:** cancelled model pulls and desktop notifications no longer leak zombie processes; shell commands run in their own process group on macOS/Linux so background grandchildren are killed with the timeout.
- **Worktree git commands have 30-second timeouts** so a blocked git (e.g. waiting on an index lock) can't hang the app.
- **Search providers cap response parsing at 1 MB**, matching the existing page-fetch limit.

### Security

- Upgraded `golang.org/x/net` (multiple CVEs, v0.33 → v0.55), `goldmark`, and AWS SDK modules past known vulnerabilities.
- Pinned `toolchain go1.25.12` for Go standard-library security backports. `govulncheck` reports zero known vulnerabilities.

### CI

- Tests now also run with `CGO_ENABLED=1` — the tree-sitter parser tests were previously never executed in CI — and with the race detector.
- `staticcheck` and `govulncheck` added to the lint job (repo is clean on both).
- Repo-wide `gofmt` fix; the format check was failing on 29 files.

### Known limitations (tracked in ROADMAP.md)

- `internal/tui` (the largest package) still has no automated tests.
- Session files grow without bound and are fully rewritten every turn; cleanup is age-based only (`session_retention_days`).
- MCP servers that die are reported but not automatically restarted.
- Process-group cleanup for shell grandchildren is a no-op on Windows.
