# Changelog

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
