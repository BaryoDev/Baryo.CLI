#!/bin/sh
# Fail if a workflow's `run:` block is not valid shell.
#
# A run block is a shell script that nothing parses until the job executes it, which is the
# most expensive moment to find a typo: the job has already set up Go, downloaded modules
# and run everything ahead of it. An unbalanced quote in the last line of a block is
# invisible to YAML validation — it is a perfectly good string — and the job fails with
# "unexpected EOF while looking for matching quote" several minutes in.
#
# That is not hypothetical: it happened to the step in ci.yml that proves the conflict
# marker check can fail, which is how this script came to exist.
#
# Syntax only. `bash -n` parses without running anything, so no command here has any effect
# on the repository or the machine.
set -eu

cd "$(dirname "$0")/.."

python3 - "$@" <<'PY'
import glob
import subprocess
import sys
import tempfile

import yaml

failures = 0
checked = 0

for path in sorted(glob.glob(".github/workflows/*.yml")):
    doc = yaml.safe_load(open(path, encoding="utf-8"))
    for job_name, job in (doc.get("jobs") or {}).items():
        # A workflow_call job has no steps of its own.
        for i, step in enumerate(job.get("steps") or []):
            script = step.get("run")
            if not script:
                continue
            # Expression interpolation is substituted by the runner before the shell sees
            # it. Replaced with a placeholder rather than left in place, because ${{ ... }}
            # is not shell syntax and would be reported as an error of its own.
            cleaned = []
            rest = script
            while "${{" in rest:
                before, _, after = rest.partition("${{")
                _, _, after = after.partition("}}")
                cleaned.append(before + "GHA_EXPR")
                rest = after
            cleaned.append(rest)
            script = "".join(cleaned)

            shell = step.get("shell", "bash")
            if shell not in ("bash", "sh"):
                continue  # python, pwsh and friends are someone else's parser

            checked += 1
            with tempfile.NamedTemporaryFile("w", suffix=".sh", delete=False) as f:
                f.write(script)
                tmp = f.name
            proc = subprocess.run([shell, "-n", tmp], capture_output=True, text=True)
            if proc.returncode != 0:
                failures += 1
                label = step.get("name") or f"step {i + 1}"
                print(f"{path}: job {job_name}: {label}")
                for line in (proc.stderr or proc.stdout).splitlines():
                    print(f"    {line.replace(tmp, '<run block>')}")

if failures:
    print()
    print(f"{failures} run block(s) are not valid shell.")
    sys.exit(1)

print(f"{checked} workflow run block(s) parse as shell.")
PY
