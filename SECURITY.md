# Security Policy

## Reporting a vulnerability

Do **not** open a public issue for anything exploitable.

- Open a private advisory: https://github.com/BaryoDev/Baryo.CLI/security/advisories/new
- Or email arnelirobles@gmail.com

Include what you found, how to reproduce it, and what an attacker gets out of it.

## Response

- **Acknowledgement:** within 48 hours
- **Initial assessment:** within a week
- **Fix:** depends on severity

## What is in scope

Baryo runs shell commands, edits files and applies diffs on a user's machine on behalf of
a language model. The interesting attack surface follows from that, and reports in these
areas are especially welcome:

- **Escaping the approval gate.** Anything that gets a destructive tool to run without the
  approval it is supposed to need, including in headless mode without `--yolo`.
- **Path traversal.** Reads or writes outside the working directory through crafted paths,
  symlinks, or `@`-mentions.
- **Command injection.** Model output, file contents, or repository data reaching a shell
  in a way the user did not intend.
- **Project-trust bypass.** A repository's own `.baryo` config, skills or hooks taking
  effect without the user trusting that project.
- **Secret disclosure.** Provider keys or credentials reaching a trace file, a session
  file, a log, or a prompt sent to a remote endpoint.
- **Supply chain.** A dependency or release artifact that is not what it claims to be,
  including checksum or attribution problems in published binaries.

## What is not a vulnerability

- A model doing something unhelpful, wrong or destructive **after** the user approved it.
  `--yolo` means what it says.
- Anything requiring the attacker to already have code execution as the user.

## Supported versions

Pre-1.0, so fixes land on `main` and in the next release. The latest release is the
supported one; there are no backports to earlier tags.
