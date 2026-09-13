# Contributing to Baryo

Thanks for being here. This file is short on etiquette and long on the two things that
actually cost contributors time: how to work from a fork, and how to get a green pipeline
on the first push.

## You will be working from a fork

Nobody outside the maintainers can push a branch to this repository, so every change
arrives as a pull request from a fork. That is the normal path, not a lesser one — the
open pull requests on this repository are fork pull requests.

```sh
# 1. Fork BaryoDev/Baryo.CLI on GitHub, then:
git clone https://github.com/<you>/Baryo.CLI.git
cd Baryo.CLI
git remote add upstream https://github.com/BaryoDev/Baryo.CLI.git

# 2. Branch from an up-to-date main.
git fetch upstream
git checkout -b fix/parser-panic upstream/main

# 3. Work, commit, push to your fork.
git push -u origin fix/parser-panic
```

Then open the pull request against `BaryoDev/Baryo.CLI` `main`.

### What that means for CI

CI runs on every branch you push **to your own fork**, not just on the pull request. Push
early and read the result there; you do not need to open a pull request to find out what
the pipeline thinks.

Two things behave differently on a fork pull request, and neither is a problem with your
change:

- **No repository secrets.** A fork's workflow run gets none, by design. Every gate in
  this pipeline is built to work without them — the secret scan, for instance, installs
  the MIT-licensed gitleaks binary rather than using the licensed Action.
- **A read-only token.** Workflows cannot write labels, comments or assignments from a
  fork pull request. The workflows that do that (`self-assign`, `flag-solicitation`) run
  on issue events in this repository instead, so they are unaffected.

If a check fails in a way that looks unrelated to your diff, say so in the pull request
rather than pushing blind retries. A failing gate that is not your fault is a bug in the
gate and worth a minute of the maintainer's attention.

## Run the gates before you push

One command runs what CI runs, in the same order:

```sh
sh scripts/ci-local.sh           # everything
sh scripts/ci-local.sh --quick   # skip race detector, cgo pass, release build
```

It keeps going after a failure and prints the full list at the end, so one run tells you
everything that needs fixing. A clean run means the pipeline has nothing new to say.

Individually, if you prefer:

```sh
gofmt -l .                                 # formatting
go vet ./...
CGO_ENABLED=0 go test -count=1 ./...       # the configuration releases are built in
CGO_ENABLED=1 go test -count=1 ./...       # the cgo tree-sitter parsers
go test -race -count=1 ./...
sh scripts/check-conflict-markers.sh
sh scripts/check-notice.sh                 # only if you changed dependencies
```

### Both cgo settings matter

`internal/index` has two parser implementations behind build tags: cgo tree-sitter, and a
pure-Go parser for the `CGO_ENABLED=0` builds that releases ship. They are separate code.
A passing test under one says nothing about the other, which is why CI runs both and so
should you.

### If you change dependencies

`NOTICE` is generated, and attribution is a licence obligation rather than a formality:

```sh
python3 scripts/gen_notice.py > NOTICE
```

The generator resolves dependencies for **both** cgo settings and merges them, because a
dependency behind `//go:build !cgo` is invisible to a plain `go list` on a machine with a
C toolchain — and that is precisely the dependency that ships in the released binary. CI
checks the committed file against a fresh run.

## What a good pull request looks like

- **One change per pull request.** A refactor bundled with a fix makes both harder to
  review and impossible to revert separately.
- **A test that fails without your change.** For a bug fix, a test that passes both with
  and without the fix proves nothing. Write it first and watch it fail, or revert the fix
  and confirm it goes red. For docs, build or dependency changes, say "not applicable".
- **Say what you left out.** Known gaps and deliberate trade-offs in the description are
  worth more than a tidy diff.
- **Match the surrounding code.** Comment density and naming in this repository are
  deliberate: comments explain why a thing is the way it is, especially when it is not
  the obvious way. `BARYO.md` carries the conventions.
- **Keep the commit message about the change.** No tooling banners, no model names, no
  sponsorship or payment details. Those are not part of the code.

## Filing an issue first

For anything larger than a fix — a new tool, a new provider, a change to how sessions or
the index work — open an issue first and let it be discussed before you build it. That is
not a gate, it is a kindness to yourself: it is much cheaper to redirect a plan than a
finished branch.

Comment `/take` on an issue to claim it. You do not need to be a collaborator, and you do
not need to wait for anyone.

## Security

Do not open a public issue for anything exploitable. See [SECURITY.md](SECURITY.md).
