# Deployment target: Baryo as a local agent appliance

Status: proposal. This document defines a deployment target that the current
architecture is close to serving, and lists the specific gaps between here and
there.

## Why this target

Baryo's differentiators — a single static binary with no language runtime, a
local-first model path with no mandatory API key, context-window-aware tool and
prompt gating, and an SSH tunnel to a remote model host — do not add up to a
better interactive coding agent than Claude Code or Cursor. They add up to
something those tools cannot do at all: **an always-on agent that runs on
hardware you own, against code that never leaves it, at zero marginal cost per
task.**

That is the target this document specs.

## What an appliance deployment looks like

A small always-on machine runs Baryo in headless (`-p`) mode on a schedule or on
a trigger. Nobody watches a TUI. Latency does not matter; unattended
correctness, bounded resource use, and never hanging do.

Representative jobs:

- Overnight review of the day's commits, written to a file or a chat webhook.
- Log and journal triage on a homelab or lab machine.
- Config and infrastructure-file review on change.
- Scheduled dependency and vulnerability summaries.
- Batch analysis of a repository that is not permitted to leave the premises.

## Topologies

### A. Split — recommended

The appliance runs Baryo; a separate box runs the model.

```
[Pi / mini PC / NAS]  --- HTTP or SSH tunnel --->  [GPU box or workstation]
   baryo headless                                    Ollama / Docker Model Runner
   repo checkout, cron
```

`internal/tunnel` already implements the SSH leg, and `socket_path` points
Baryo at any OpenAI-compatible local endpoint. The appliance stays cheap,
low-power and always-on; inference runs on hardware that can actually do it.

### B. All-in-one

Model and Baryo on the same machine. Simplest to deploy, and the only option
when the network is genuinely air-gapped. Viable only with adequate hardware —
see below.

## Hardware guidance

Agentic work is **prefill-bound, not generation-bound**. Every tool round
re-sends the full context: system prompt, repo map, tool definitions and
history. Prompt processing throughput, not tokens per second of generation, is
what determines whether a deployment is usable.

| Class | Example | Verdict |
|---|---|---|
| Raspberry Pi 4/5, CPU-only | Pi 5 8GB | Good **appliance host** (topology A). Poor **inference host**: a multi-thousand-token agent prompt spends minutes in prefill, and every tool round pays it again. |
| Mini PC, 32GB, CPU-only | N100 / Ryzen mini | Workable all-in-one for batch jobs with 3B-7B models. |
| SBC with NPU/GPU | Jetson Orin | Good all-in-one. |
| Desktop with a discrete GPU | any 12GB+ card | Best inference host for topology A. |

A Pi is a fine place to run the harness. It is a poor place to run the model.
Recommending otherwise sets users up to conclude the tool is broken when the
hardware is the constraint.

## The headless path

Print mode is the appliance interface. Unlike the interactive TUI, it already
accepts a configurable round budget:

```sh
baryo -p "review the commits since yesterday and summarize regressions" \
      --yolo \
      --max-turns 25 \
      --output json
```

- `-p` — non-interactive; streams to stdout.
- `--max-turns` — tool-call round budget (`internal/cli/cli.go:82`). The
  interactive TUI is hard-capped at 5 (`internal/llm/toolloop.go:19`); print
  mode is not. Appliance jobs should set this explicitly.
- `--output json` — structured result for scripting.
- `--yolo` — auto-approves destructive tools. Unattended jobs require it, which
  is precisely why the trust and containment gaps below are blocking rather
  than cosmetic.
- stdin is accepted as context, so `journalctl … | baryo -p "triage this"` works.

### Scheduling

A systemd timer is preferred over cron: it gives the job a memory cap, a
wall-clock timeout, and journal capture, all of which matter on constrained
hardware.

```ini
# /etc/systemd/system/baryo-review.service
[Unit]
Description=Baryo nightly review

[Service]
Type=oneshot
User=baryo
WorkingDirectory=/srv/repo
Environment=HOME=/var/lib/baryo
ExecStart=/usr/local/bin/baryo -p "review today's commits" --yolo --max-turns 25 --output json
StandardOutput=append:/var/log/baryo/review.jsonl
# Bound the blast radius on small hardware:
RuntimeMaxSec=3600
MemoryMax=2G
```

`RuntimeMaxSec` is load-bearing today, because Baryo has no stream timeout of
its own (issue #7): a stalled model endpoint hangs the process indefinitely.

## Build and packaging: the CGO question

**Released binaries ship a non-functional repo map.** `.goreleaser.yaml` sets
`CGO_ENABLED=0`. Tree-sitter requires CGO, so `internal/index/parser_nocgo.go`
is compiled in, and its `ParseFile` returns an error for every file. That error
propagates through `Index.parseOne`, and both `Build` and `Update` skip any file
whose parse failed — so the index ends up empty rather than symbol-less.

Measured on this repository:

| Build | Files discovered | Files indexed | Repo map |
|---|---|---|---|
| `CGO_ENABLED=1` | 175 | 175 | 5780 bytes, symbols extracted |
| `CGO_ENABLED=0` | 175 | **0** | **empty** |

This affects every distribution channel — Homebrew, Scoop, `install.sh` — on
every platform, not only ARM. It matters most in an appliance deployment, where
a small model's limited context makes an accurate repo map the highest-value
thing you can spend tokens on.

Recommended sequence:

1. **Stop the bleeding (small).** Have the no-CGO `ParseFile` return its
   metadata with a `nil` error so files are indexed without symbols. The repo
   map degrades to a file listing — which is still useful context — instead of
   vanishing. Surface the degraded state in `baryo doctor`, which currently
   reports nothing about CGO or tree-sitter.
2. **Ship real symbols.** Build release artifacts with CGO enabled. `zig cc` as
   the cross-compiler is the cleanest path: one toolchain cross-compiles
   `linux/amd64` and `linux/arm64` against musl, producing static binaries with
   no glibc version floor — which preserves the "single portable binary"
   property that makes appliance deployment easy. macOS builds use the native
   clang on a macOS runner. Windows can stay no-CGO with the degraded path from
   step 1.
3. **Guard it.** CI builds the binary only with `CGO_ENABLED=0` today, and the
   CGO test pass does not assert that indexing produces symbols. Add a test that
   fails if a CGO build indexes zero files.

## Blocking gaps

Ordered by how hard each one bites in this deployment.

| Gap | Impact on an appliance | Reference |
|---|---|---|
| Repo map dead in released binaries | The model runs without repo structure exactly where context is scarcest | [#9](https://github.com/BaryoDev/Baryo.CLI/issues/9) |
| `git check-ignore` forked once per file | `ignore.IsIgnored` spawns a process per path; a `grep` over a few thousand files spends minutes spawning processes before the model sees anything, and constrained CPUs and SD cards make it far worse | [#10](https://github.com/BaryoDev/Baryo.CLI/issues/10) |
| No stream timeout | An unattended job hangs forever on a stalled endpoint; currently survivable only via `RuntimeMaxSec` | [#7](https://github.com/BaryoDev/Baryo.CLI/issues/7) |
| Session file rewritten in full every turn | Write amplification on SD and eMMC storage in an always-on deployment | [#11](https://github.com/BaryoDev/Baryo.CLI/issues/11) |
| Project config and skills are trusted implicitly | An appliance running `--yolo` against repositories it did not author executes whatever `.baryo/config.yaml` and `skills/` in that repo specify | [#12](https://github.com/BaryoDev/Baryo.CLI/issues/12) |
| Interactive round cap of 5 | Does not block headless jobs (`--max-turns`), but makes the TUI unsuitable for verifying appliance jobs by hand | `internal/llm/toolloop.go:19` |

## Definition of done

This target is credible when all of the following hold:

1. A released `linux/arm64` binary indexes a repository and produces a non-empty
   repo map.
2. A headless run over a 5,000-file repository spends under 5 seconds in path
   filtering.
3. A job against a stalled endpoint exits non-zero within a configured timeout
   rather than hanging.
4. `--yolo` against an untrusted repository does not execute code from that
   repository without consent.
5. A documented systemd unit runs a nightly job unattended for a week on 8GB
   ARM hardware without manual intervention.
