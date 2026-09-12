# Baryo: cloud delegation, local migration, and the braindb

Date: 2026-09-12
Status: design, awaiting review
Reviewed code at: `e8412e3` (main)

## 1. What we are building

Baryo delegates a hard task to a cloud model, records how that task was actually
solved, distills the recording into a reusable procedure, and then runs the same
class of task on a local model by handing it that procedure. Each procedure keeps
a score. Once a procedure is reliable, that class of task stops going to the cloud.

The store of procedures plus their scores is the braindb.

Two things motivate it, and they turn out to be the same thing. Capability: a small
local model cannot plan a multi-step task it has never seen, but it can follow a
procedure. Speed: Baryo currently spends 4,656 tokens of speculative context per
turn on an 8K model before the user's first word, and a matched procedure replaces
most of that with a few hundred tokens that are actually relevant.

## 2. Non-goals and honest limits

- **No fine-tuning, no distillation into weights.** No local GPU worth training on,
  and Docker Model Runner has no clean adapter-serving path. Procedures are text.
- **Procedures only help tasks that repeat in shape.** Release notes, a known class
  of failing test, adding the Nth instance of an existing pattern: these migrate
  local. Novel design work does not, at any score. The braindb makes a small model
  reliable at repeat work, not clever.
- **No semantic cache.** Replaying a stored answer is reuse, not learning, and it
  goes stale silently when the repo moves on.
- **Not a multi-agent framework.** One task, one procedure, one model at a time.

## 3. Prior art

The design shape is published and measured. Agent Workflow Memory (arXiv 2409.07429,
ICML 2025) induces reusable workflows from past trajectories and injects the relevant
ones into later runs, offline from demonstrations or online from self-generated
successful trajectories. Reported gains: +24.6% relative success on Mind2Web, +51.1%
relative on WebArena, with fewer steps per solved task, holding up as train and test
distributions diverge (8.9 to 14.0 absolute points over baselines).

What that paper leaves vague is the part that matters most here: it does not define
what qualifies a trajectory as successful, or whether a verifier is involved. In web
navigation a bad workflow wastes clicks. In a code repo it writes wrong code. So the
filter is the feature, not a detail.

On the judge: research is consistent that smaller and open judges misclassify
incorrect code as correct, that judges favour outputs from their own family, and that
the mitigations are a different-family judge and pairwise scoring in both orderings
(arXiv 2410.21819, arXiv 2510.24367, arXiv 2604.16790). That shapes section 8.

## 4. Current state: what the code does today

Verified at `e8412e3`. These are the constraints the design has to live with.

**There is no orchestrator.** `internal/tui/chat.go` is 5,794 lines, 23% of all
non-test code, and holds provider endpoint resolution (`endpointForModel:396`), about
45 slash-command handlers, intent classification, prompt assembly and budgeting,
token estimation, skill activation, venv and pip installs, compaction, session saving,
and the view. The Bubble Tea model is the orchestrator.

**Headless is a second, weaker implementation.** `internal/cli/print.go:259`
`buildMessages` injects the system prompt and the meta-tools block only. The TUI's
`buildMessagesWithToolGating:2939` injects memories, RAG, pinned files, repo map,
skills index, mode prompts, MCP guidance and small-model reminders. There are two
`mcpContextWindow` functions and two executors. Anything written into the TUI does
not reach `baryo -p`.

**Nothing records what happened.** After a tool loop, `chat.go:1114` appends one
message: the concatenated narration. Tool calls, arguments and results are executed
inside `llm.StreamChatWithTools` and discarded. Confirmed against real data: 0 of 184
session files on this machine contain a single `tool_calls` entry. Saved message
objects have exactly two keys, `role` and `content`.

**The permission switch exists four times.** `chat.go:3223`, `chat.go:3278`,
`print.go:305`, and inside `metatools.go`. MCP tools are routed before all of them
(`chat.go:3211`, `print.go:302`), so in the default `confirm` mode a third-party MCP
write tool runs with no prompt, and `mcp_in_read_only` defaults to true so plan, ask,
architect and review modes execute MCP tools unprompted.

**Project config is trusted implicitly.** `config/config.go:222` merges
`./.baryo/config.yaml` with no gate, including `hooks` (run via `sh -c` at
`tui/hooks.go:84`), `mcp_servers` (spawned at `main.go:201`), `permission_mode` and
`ssh_tunnel`. Cloning a repo and running baryo executes that repo's code.

**The prompt prefix is poisoned every request.** `tools.AllDefinitions:87` iterates a
`map[string]Tool`, so the tools array order is random per request: 6 consecutive calls
produced 5 distinct orderings. MCP definitions (`mcp/manager.go:134`) and pinned
context (`chat.go:2510`) iterate maps too. Local servers reuse the KV cache only on a
byte-identical prefix, so the tools block and the whole history are re-prefilled every
turn.

**Released binaries ship an empty index.** `index/parser_nocgo.go:44` returns an error,
`index.go:52` skips any file whose parse errored, and `.goreleaser.yaml:15` sets
`CGO_ENABLED=0`. Retrieval is broken in exactly the artifact users install (issue #9).

## 5. Phases and gates

Each phase has a gate. A phase that fails its gate stops the project there.

### Phase 0: foundation

Unit 0 first, because it is ten lines and the largest wall-clock win available:
sort the three map iterations (`tools.go:87`, `tools.go:126`, `tools.go:111`,
`mcp/manager.go:134`, `chat.go:2510`) and default `rewrite` to false for local
endpoints (`config.go:45`).

Then, in order:

1. Release and docs hygiene: #4 (reusable `workflow_call` so release runs tests),
   #5 (checksum verification plus the `chmod` bug on the sudo path), #6 (delete the
   impossible `go install` line and fix the stale version), add the govulncheck
   `schedule:` trigger, close #2 as fixed by #3.
2. Policy gate: #12 trust store, findings A and F, `permission_mode` validation, the
   `.venv` marker. **Output: one policy object** that later replaces four copies of
   the permission switch.
3. Path and stream safety: #8 symlink resolution, #7 idle watchdog plus `--timeout`.
4. Ignore batching: #10, including the NUL-separator bug at `tui/mention.go:105` that
   means @mention has never filtered ignored files.
5. Index correctness (#9) and the trajectory recorder (section 6).
6. Prompt ordering and trimming: volatile blocks after stable ones, constant system
   prompt in dynamic chat mode. Behaviour-risky, so A/B against the real model after
   Unit 0 proves caching works.

Deferred: #11 as filed. Tens of KB per turn is far below SD endurance, and the journal
shape should be designed once, with the orchestrator's persistence.

Not doing: the module rename half of #6, CGO cross-compilation via zig for #9.

**Gate:** govulncheck clean on a schedule, release workflow runs tests, a released
binary produces a non-empty repo map, MCP tools honour the permission gate, and a
completed task writes a trajectory file.

### Phase 1: measure

A constructed benchmark, not archaeology. The session history cannot be mined: it
holds no tool traces, 174 of 184 sessions ran in a checkout that no longer exists, and
only 3 have an engineering shape.

Build 20 tasks from repos with fast deterministic suites (`BaryoVM` in Go, `Verdict`
or `rnxORM` in .NET). Each task is a tuple: parent SHA as start state, the commit
message or linked issue as the prompt, the package-scoped test command as the verify,
and the real commit as a reference solution. This gives a machine-checkable oracle on
every task for free, so phase 1 does not depend on the judge at all.

Three arms per task: local alone, local with a hand-distilled procedure, cloud alone
as the ceiling. Record verify pass or fail, wall clock, and tokens.

Procedures in this phase are written by hand. No Baryo code changes.

**Gate:** the procedure arm must beat the bare local arm by at least 25 percentage
points on the repeat-shaped subset. Below that, stop: phases 2 and 3 are weeks of work
that only pay off if this number is real.

### Phase 2: extract

`internal/agent` owns a task lifecycle, one context builder, one executor over the
phase 0 policy object, and the router. `internal/tui` and `internal/cli` become front
ends over one event stream. Endpoint resolution moves from `tui/chat.go:396` to
`internal/llm`. Tests land with the extraction, since `internal/tui` and
`internal/cli` have none today.

Strangler, in this order: context builder first (it is the duplication that hurts
most), then the executor, then the lifecycle, then the command handlers.

**Gate:** `baryo -p` and the TUI provably produce an identical prompt for identical
input, asserted by a test.

### Phase 3: braindb

`internal/brain`: store, retrieval, judge, promotion. Sections 6 to 10.

**Gate:** procedures demote as reliably as they promote, and no project-supplied
procedure is ever auto-trusted.

## 6. Trajectory format

Append-only JSONL at `~/.baryo/traces/<session-id>.jsonl`, one record per event.
Written by the recorder in phase 0, consumed by the distiller in phase 3.

```
{"t":"task_start","id":"...","ts":"...","prompt":"...","model":"...","endpoint":"local|<provider>","cwd":"...","repo":{"head":"<sha>","dirty":true}}
{"t":"tool_call","id":"...","round":1,"name":"read_file","args":{...}}
{"t":"tool_result","id":"...","round":1,"name":"read_file","bytes":1843,"is_error":false,"content":"<capped>"}
{"t":"diff","id":"...","unified":"<capped at 64KB>"}
{"t":"verify","id":"...","command":"go test ./internal/llm","exit_code":0,"output":"<capped>"}
{"t":"task_end","id":"...","outcome":"verified|failed|unknown","usage":{"prompt":0,"completion":0},"wall_ms":0}
```

Three rules that are not negotiable:

- **The trace is never injected into the prompt.** `m.messages` is the prompt. Today
  the tool loop keeps tool traffic round-local and commits only the narration, which
  is the right trade for small models: persisting tool results into history would
  silently change every subsequent turn and can push real context off the end of an
  8K window. The recorder is a parallel sink, not a history change.
- **Append, never rewrite.** Avoids issue #11's write amplification by construction.
- **Redact before write.** Tool arguments and results pass a redactor (provider keys
  from `config.yaml`, `BARYO_*` env values, anything matching a key-shaped token).
  A trace is the richest secret-bearing artifact Baryo would produce.

## 7. Recipe schema

Stored as one JSON line per recipe in the store (section 10). Shown here as YAML
only because it reads better on a page.

```yaml
id: <sha256 of normalized trigger>
trigger:
  intent: "add a provider adapter"
  repo_fingerprint: <hash of module path + top-level layout>
  signals: ["internal/llm/*.go", "go", "read_file", "write_file"]
steps:
  - "read internal/llm/provider.go, find the registry table"
  - "copy the shape of anthropic.go"
  - "add an entry to the providers map and to model_hints.go"
  - "add a provider_test.go case"
files: ["internal/llm/provider.go", "internal/llm/model_hints.go"]
verify:
  command: "go test ./internal/llm"
  kind: machine          # machine | judge | none
provenance:
  teacher: "claude-sonnet-5"
  teacher_provider: "anthropic"
  trajectory_id: "..."
  created_at: "2026-09-12T14:00:00Z"
score:
  attempts: 9
  passes: 7
  source: machine        # machine | judge | mixed
  state: LOCAL           # CLOUD | SHADOW | LOCAL
  demotions: 1
  last_result: pass
  last_seen_head: "<sha>"
trust: global            # global | project
```

`verify.kind: none` means the recipe is retrievable but **never promotable**. It can
still displace speculative context, which is a real win on its own.

## 8. Routing and scoring

State machine per recipe, not per model:

```
(no recipe)  -> CLOUD
CLOUD        -> SHADOW   when a trajectory verifies and a recipe is distilled
SHADOW       -> LOCAL    after 5 consecutive passes (machine) ; judge-only stays in SHADOW
                         unless the user promotes it explicitly
LOCAL        -> SHADOW   on the first failure
SHADOW       -> CLOUD    after 3 failures inside the shadow window
any          -> SHADOW   when freshness fails (a named file is gone, or HEAD moved and the recipe has not run since)
```

In SHADOW the local model runs first; if verify fails, the turn falls back to cloud
so the user still gets one good answer. That makes the learning window cheap to sit
in and honest about its cost: the user sees "local attempt failed, escalated".

Judge policy, from the research in section 3:

- A machine check wins whenever a verify command exists. The judge is only consulted
  for `verify.kind: judge`.
- The judge must be a different family from **both** the teacher and the local
  student. Config validation refuses a judge that matches either, and a recipe whose
  judge violated that rule cannot be promoted.
- Pairwise, both orderings, when a reference solution exists.
- `score.source` is stored and surfaced. A judge-scored recipe caps at SHADOW. Only an
  explicit user promotion moves it to LOCAL, and that is recorded as such.

## 9. Trust boundary

The braindb is a new place for untrusted text to enter a prompt, and finding B is that
mistake already shipped in this repo. So:

- `~/.baryo/brain/` is trusted. `./.baryo/brain/` is **not loaded at all** unless the
  project is trusted via the phase 0 trust store, and project recipes are out of scope
  for v1.
- A recipe's `steps` are prose injected into a prompt. They are a prompt-injection
  carrier by construction. A recipe can therefore never add a tool, change the
  permission mode, alter the agent mode, or reference an MCP server.
- `verify.command` is the one executable field. It passes the same gate as
  `run_command`, it is never auto-run from a recipe whose trust is not global, and in
  `confirm` mode its first run per recipe is confirmed by the user.
- Provenance is mandatory. A recipe with no teacher and no trajectory id is invalid.

## 10. Storage

Append-only JSONL at `~/.baryo/brain/recipes.jsonl`, loaded into memory at startup,
indexed with the existing BM25 implementation in `internal/rag`. No new dependency,
no CGO question, no sqlite.

Sizing: a few hundred recipes at roughly 1KB each. BM25 over short text is the right
retrieval for this, and embeddings would add a model dependency to buy very little.

Retrieval is a two-stage filter: `repo_fingerprint` must match, then BM25 over
`intent` plus `signals` against the user's message, then freshness. Top 1 wins;
injecting two competing procedures is worse than injecting none.

## 11. Target package layout after phase 2

```
internal/agent    task lifecycle, context builder, router, executor over policy, event stream
internal/brain    recipe store, retrieval, distiller, judge, scoring
internal/llm      providers and endpoint resolution (endpointForModel moves here)
internal/policy   permission mode, project trust, hooks, MCP gating (phase 0 unit 2)
internal/tui      view only, consumes agent events
internal/cli      printer only, consumes agent events
```

## 12. Open decisions

1. **#9 symbol extraction.** Ship the cheap fix now (stop discarding every file, repo
   map degrades to paths, doctor reports symbols unavailable) and spike the pure-Go
   tree-sitter runtime, or take on a cgo release matrix. The pure-Go candidate is MIT
   and covers the needed languages but was released 2026-09-05, so it is a spike.
   Recommendation: cheap fix plus spike.
2. **Judge provider.** Must differ from both teacher and student. If the teacher is
   Claude and the student is a local Qwen, the judge is something else again.
3. **Project-level recipes.** Recommendation: not in v1.

## 13. What success looks like

A task class that needed a cloud call in September runs locally in November, at a
measured pass rate, with the evidence for that claim re-runnable on demand. And the
per-turn token overhead on an 8K model drops from 4,656 to something in the hundreds
when a recipe matches, because the procedure displaces the speculation instead of
adding to it.
