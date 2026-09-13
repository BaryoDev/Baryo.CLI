# Baryo: plugin architecture, and history export as its first case

Date: 2026-09-13
Status: stages 1-5 implemented; stage 6 and the open questions in §10 outstanding
Reviewed code at: `d454fa8` (main)

**Implementation status.** §8 stages 1 and 2 (the archive envelope, and archiving the
compaction paths that were dropping content) and stages 3 to 5 (`baryo export`, plugin
manifests and discovery, the `exporter` kind) are implemented. Stage 6 (`tool`, `hook` and
`provider` kinds) is not. Where this document and the code disagree, the code is right and
this document is the bug; §6 and §3.1 have already been corrected once on that basis.

## 1. What this is for

Two requests arrived together, and they turn out to be one request.

The first is from Luca (ctx): expose Baryo's conversation history so a retrieval tool can
search earlier sessions and pull back the original messages and tool calls, via an adapter
that writes ctx's documented `ctx-history-jsonl-v2` format.

The second is the standing goal in `ROADMAP.md` §"Plugin System": let users add what they
want without waiting for it to be merged here.

Building the ctx adapter into the binary would satisfy the first and make the second
harder — one more integration with a privileged position in the core, and the next
retrieval tool asks for the same. Building the extension point and making ctx its first
consumer satisfies both. That is what this proposes.

## 2. Correcting the premise: the archive already exists

Luca's message identifies the blocker as "the missing pre-compaction archive ... if Baryo
saves only the compacted list, ctx can't recover the discarded messages."

That is no longer true, and it changes the size of the work substantially. Reading
`internal/session/session.go` and `internal/tui/chat.go` at `d454fa8`:

- `Session.Archive(messages)` appends to `~/.baryo/sessions/<id>.archive.jsonl`, one JSON
  message per line, `O_APPEND`, mode `0600` (`session.go:107`).
- Compaction calls it **before** discarding anything: `chat.go:1044` archives
  `m.messages[:keep]` and only then replaces them with the summary.
- `LoadArchive(id)` reads it back (`session.go:137`), and `/sessions` search already covers
  archived content through `archiveMatches` (`session.go:165`), so compacted-away messages
  are searchable today.
- The archived records are `llm.ChatMessage`, which carries `ToolCalls []ToolCall` and
  `ToolCallID` — so tool calls and tool results survive, not just prose.
- Separately, `<id>.trace.jsonl` (`internal/trace`) records `tool_call`, `tool_result`,
  `diff`, `verify` and `task_end` records, each with an RFC 3339 `ts`, with secret-shaped
  strings redacted before writing.

So the durable evidence Luca wants preserved is already on disk, in two files, in a
documented shape. What is missing is not the archive. It is an **exporter**, plus four
gaps in archive coverage that are worth fixing on their own merits.

## 3. The four real gaps

These were found by reading the code rather than by running an export, so each needs
confirming against a real session before it is fixed.

**3.1 Archived messages have no timestamps.** `llm.ChatMessage` has no time field, so an
archive line cannot say when its message happened. A ctx `event` record requires
`occurred_at`. Today the best available answer is interpolation between the session's
`CreatedAt` and `UpdatedAt`, or a join against `<id>.trace.jsonl`, which is timestamped but
only covers tool activity. Neither is faithful.

Fix: wrap each archive line in an envelope, `{"ts": ..., "seq": ..., "msg": {...}}`.
Backward compatible, and the reader accepts both shapes — an old bare-message line yields a
zero `ts`, which the exporter reports as `fidelity: "partial"` rather than guessing.

Worth being exact about what that `ts` is: **the time the message was archived, not the time
it was sent.** `llm.ChatMessage` has no timestamp, so compaction is the earliest point where
one can honestly be attached, and it bounds the message from above rather than dating it.
That is a real improvement over nothing — it orders records and bounds them within a session
— but recording true send times means timestamping messages where they are created, which is
a separate change to the conversation model and is not in this one.

**3.2 Only one of four compaction paths archives.** `chat.go:1044` is the sole `Archive`
call. The search (`chat.go:1162`), research (`chat.go:1181`) and strategy paths each
truncate bulky content in place — `content[:500] + "... (full content summarized by
assistant above)"` — and the original is gone. `/clear` (`chat.go:1968`) drops the whole
message list, and the error-retry path (`chat.go:1367`) drops the last user message. Every
one of those is discarded evidence of exactly the kind ctx exists to recover.

**3.3 Retention deletes the evidence.** `CleanOld` removes `<id>.archive.jsonl` and
`<id>.trace.jsonl` along with the session. Correct as a privacy default, wrong if a
retrieval index is meant to outlive the session. Needs either a separate retention setting
for archives or an export-before-delete hook.

**3.4 Headless mode has no session.** `baryo -p` produces a trace when `--trace-file` is
given but never a session or an archive, so scripted runs contribute nothing to history.

## 4. Why not Go's `plugin` package

Worth settling early, because it is the obvious first idea and it is a trap here.

`-buildmode=plugin` requires cgo, requires the plugin and host to be built with the same
toolchain version and byte-identical versions of every shared dependency, and does not work
on Windows. This project has just spent PR #47 getting tree-sitter working **without** cgo
precisely so released binaries are static and portable. A `.so` ABI would hand that back
and add a support burden ("your plugin was built with Go 1.25.1, this binary is 1.25.0").

So: **plugins are separate programs.** Baryo speaks to them over stdin/stdout with JSON,
one request per line. That costs a process spawn, which is irrelevant for every extension
point proposed here, and it buys language independence — a ctx adapter in Rust, a tool in
Python, a hook in shell all work the same way.

This is also already the shape of two existing mechanisms: hooks are shell commands
(`internal/tui/hooks.go`), and MCP servers are subprocesses speaking JSON-RPC
(`internal/mcp`). The plugin contract should look like those, not like something new.

## 5. The extension points

Five kinds. Four already exist in some form, and the work is mostly unification rather than
invention — which is the point: a plugin system that does not subsume the mechanisms
already in the tree just adds a sixth way to extend Baryo.

| Kind | Exists today as | What changes |
|---|---|---|
| `tool` | `internal/tools` (compiled in), MCP servers | Declared tools from a manifest, no rebuild |
| `hook` | `internal/tui/hooks.go`, 6 events, shell + `BARYO_HOOK_*` env | Plugins subscribe to events; more events |
| `skill` | `default-skills/`, `skills/`, `.baryo/skills/` | Skills ship inside a plugin bundle |
| `provider` | `internal/llm` endpoint resolution | A plugin resolves a model tag to an endpoint |
| `exporter` | **nothing** | Reads sessions, archives and traces; writes a format |

The ctx adapter is an `exporter`. It is the right first one to build: it is read-only, so a
bug in it cannot corrupt a session or run a command, and it has a real external consumer
to validate against.

## 6. Plugin layout and manifest

```
~/.baryo/plugins/<name>/plugin.yaml     # user's own, trusted by virtue of being there
.baryo/plugins/<name>/plugin.yaml       # project-supplied, requires --trust-project
```

```yaml
name: ctx-history
version: 0.1.0
description: Export Baryo sessions to ctx-history-jsonl-v2 for retrieval by ctx.
# Everything the plugin can do is declared here. Nothing is implicit: a plugin that
# declares only an exporter cannot register a tool at runtime.
provides:
  - kind: exporter
    id: ctx-history-jsonl-v2
    # Invoked as: <command> [args...] with a JSON request on stdin. Args are passed
    # verbatim — there is no variable substitution, deliberately: the output directory
    # arrives in the request's out_dir, so there is exactly one place it comes from and
    # no second one to disagree with it.
    command: ./baryo-ctx-export
    args: ["--verbose"]
# Declared and displayed, so a user can read what a plugin asks for before trusting it.
# NOT enforced today: see below.
permissions:
  sessions: read      # ~/.baryo/sessions, including archives and traces
  network: none
  filesystem: write   # the request's out_dir
```

Three properties matter more than the exact field names:

- **Capabilities are declared, not discovered.** The host knows what a plugin can do before
  running it, so `baryo plugins list` can show it and a reviewer can read it.
- **Permissions are declared and displayed, not yet enforced.** A plugin runs with the same
  access as Baryo itself, and `baryo plugins inspect` says so in as many words. Enforcing
  them needs fail-closed isolation on every supported OS, which `internal/sandbox` does not
  provide — it wraps execution in Docker and errors out when Docker is absent, so it cannot
  be the mechanism for something that must work everywhere. Until that exists, `network:
  none` is a statement of intent a reviewer can check the plugin against, not a guarantee
  the host makes. A permissions block that reads as enforced and is not would be worse than
  no permissions block at all.
- **Project plugins ride the existing trust gate.** `internal/config/trust.go` already
  decides whether a cloned repository's `.baryo` config and skills take effect, with
  `--trust-project` and a remembered decision. A project-supplied plugin is strictly more
  dangerous than a project-supplied skill and must not get an easier path. Default: ignored
  with a notice, exactly as project skills are today (`main.go:487`).

## 7. The exporter contract

Request on stdin, one JSON object:

```json
{
  "schema": "baryo.exporter.v1",
  "sessions_dir": "/home/u/.baryo/sessions",
  "sessions": [
    {
      "id": "a1b2c3d4e5f60718",
      "title": "fix the parser panic",
      "model_tag": "qwen2.5-coder:7b",
      "cwd": "/home/u/src/proj",
      "created_at": "2026-09-12T09:13:49Z",
      "updated_at": "2026-09-12T11:40:02Z",
      "messages_path": "/home/u/.baryo/sessions/a1b2c3d4e5f60718.json",
      "archive_path": "/home/u/.baryo/sessions/a1b2c3d4e5f60718.archive.jsonl",
      "trace_path": "/home/u/.baryo/sessions/a1b2c3d4e5f60718.trace.jsonl"
    }
  ],
  "since": "2026-09-01T00:00:00Z",
  "out_dir": "/home/u/.local/share/ctx/imports/baryo"
}
```

Response on stdout:

```json
{"ok": true, "written": ["/home/u/.local/share/ctx/imports/baryo/baryo.jsonl"],
 "sessions": 12, "events": 3480, "warnings": ["3 sessions predate archive timestamps"]}
```

Baryo passes paths, not content. A session with a large archive should not be marshalled
through a pipe, and the plugin may want to stream it.

### Mapping to `ctx-history-jsonl-v2`

From ctx's `docs/custom-history-import-format.md` at v1.4.1, the mapping is mechanical,
which is the main evidence that this extension point is the right shape:

| ctx record | Required fields | From Baryo |
|---|---|---|
| `manifest` | `record_type`, `schema_version` | constant; `producer` = `baryo/<version>`, `exported_at` = now |
| `source` | `source_id`, `provider_key`, `source_format` | `provider_key: "baryo"`, `source_id` per machine + sessions dir, `trust: provider_export` |
| `session` | `source_id`, `provider_session_id`, `started_at` | session `ID`, `CreatedAt`; `cwd`, `ended_at` = `UpdatedAt` |
| `event` | `source_id`, `provider_session_id`, `event_index`, `occurred_at` | one per message, archive first then live, `event_index` = position; `role`, `payload` = the `ChatMessage`; `event_type` = `message` / `tool_call` / `tool_result` |
| `file_reference` | ... `value`, `event_index` | paths from `tool_call` arguments in the trace |
| `edge` | `from`/`to_provider_session_id` | `/clear` and resume transitions, once those are recorded |

Two honest limits to carry into the export rather than paper over:

- `occurred_at` is only faithful after gap 3.1 is fixed. Until then those records are
  `fidelity: "partial"` — ctx has the field, and using it correctly is better than
  inventing timestamps.
- Ordering across the archive/live boundary is reconstructible (the archive is append-only
  and compaction appends in order), but there is no explicit sequence number. `seq` in the
  3.1 envelope removes the inference.

## 8. Staging

Smallest useful thing first, and each stage ships on its own.

1. **Archive envelope with `ts` and `seq`** (gap 3.1). Small, backward compatible,
   independently valuable: it makes `/sessions` search results datable.
2. **Archive the other three compaction paths** (gap 3.2). Pure bug fix. Arguably should
   not wait for any of this.
3. **`baryo export --format <id>` with one built-in format** (`baryo-jsonl`, the native
   shape). Establishes the command and the exporter contract with no plugin loading.
4. **Plugin discovery and manifests**, `exporter` kind only, behind the existing trust gate.
   `baryo plugins list`, `baryo plugins inspect <name>`.
5. **The ctx adapter** as an external plugin in its own repository, validated by importing
   into a real ctx install. If it needs nothing from the core that stage 4 did not provide,
   the contract is right.
6. **`tool` and `hook` kinds**, reusing the same manifest. `provider` last: model and
   endpoint resolution is the part most likely to change under us.

## 9. What to tell Luca

The archive exists, has existed since before the message, and keeps tool calls as well as
messages — so the blocking constraint he identified is not there. The remaining work is an
adapter, which is what he proposed, plus four coverage gaps in §3 that we own.

Worth being specific with him about two things, because they affect what ctx can promise
its users: archived messages carry no timestamps yet (§3.1), and session retention
currently deletes archives (§3.3). An adapter written today would import with
`fidelity: "partial"` on older sessions. Both are ours to fix, and stage 1 of §8 fixes the
first.

## 10. Open questions

- **Does an exporter need to be a plugin at all, or is a documented on-disk format
  enough?** The archive is already JSONL that any external tool can read. The honest case
  for an exporter plugin is that the mapping to a third-party schema is real work that
  should not live in this repository, and that `baryo export` is a better contract than
  "read our files and hope the shape holds". But a documented, versioned format plus no
  code at all is a legitimate alternative, and cheaper.
- **Is `provider` worth a plugin kind**, or does that just invite a long tail of half-working
  endpoints that get reported as Baryo bugs?
- **Signing and distribution.** A registry (`ROADMAP.md` mentions one) implies provenance.
  Deferred, but deciding it late means migrating plugins that already exist.
- **What enforces `permissions`?** `internal/sandbox` is Docker-based and fails when Docker is
  absent, so it cannot back a promise that must hold on every supported OS. Options are a
  per-OS mechanism (seccomp or namespaces on Linux, sandbox-exec on macOS, a job object on
  Windows), requiring Docker for plugins that declare restrictions, or leaving the fields
  advisory and saying so — which is the current state. Until one is chosen, the README and
  `plugins inspect` must keep saying the fields are not enforced.
