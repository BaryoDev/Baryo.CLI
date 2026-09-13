#!/usr/bin/env python3
"""Regenerate NOTICE from the modules actually linked into the binary.

Run after changing dependencies:

    python3 scripts/gen_notice.py > NOTICE

The lint job checks the committed file matches, so attribution cannot go stale.
"""
import hashlib
import os
import re
import subprocess
import sys

PROJECT = "Baryo CLI"
COPYRIGHT = "Copyright (c) 2025-2026 Arnel Robles"
# Checked in order: titles first, then distinctive body text, because plenty of
# licence files carry only the copyright line and the body.
LICENCE_NAMES = [
    (r"MIT License", "MIT"),
    (r"Apache License", "Apache-2.0"),
    (r"ISC License", "ISC"),
    (r"Mozilla Public License", "MPL-2.0"),
    (r"GNU GENERAL PUBLIC", "GPL"),
    (r"Permission to use, copy, modify, and/or distribute", "ISC"),
    (r"Permission is hereby granted, free of charge", "MIT"),
    (r"name of .*? nor the names of its\s+contributors", "BSD-3-Clause"),
    (r"Redistribution and use in source and binary forms", "BSD-2-Clause"),
]


def modules():
    """Return (path, version, dir) for every module linked into the binary under either cgo or pure-Go."""
    seen = {}
    for cgo in ("0", "1"):
        env = os.environ.copy()
        env["CGO_ENABLED"] = cgo
        out = subprocess.run(
            ["go", "list", "-deps", "-f",
             "{{with .Module}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}", "."],
            capture_output=True, text=True, check=True, env=env).stdout
        for line in out.splitlines():
            if not line.strip():
                continue
            parts = line.split("\t")
            if len(parts) != 3:
                continue
            path, version, directory = parts
            if path == "github.com/arnelirobles/baryo-cli" or not directory:
                continue
            seen[path] = (version, directory)
    return sorted((p, v, d) for p, (v, d) in seen.items())


def licence_file(directory):
    for name in sorted(os.listdir(directory)):
        if re.match(r"^(LICENSE|LICENCE|COPYING)", name, re.I):
            full = os.path.join(directory, name)
            if os.path.isfile(full):
                return full
    return None


def classify(text):
    for pattern, name in LICENCE_NAMES:
        if re.search(pattern, text, re.S):
            return name
    return "see text"


def notice_file(directory):
    for name in sorted(os.listdir(directory)):
        if re.match(r"^NOTICE", name, re.I):
            full = os.path.join(directory, name)
            if os.path.isfile(full):
                return full
    return None


# Grammars compiled into github.com/odvcencio/gotreesitter for the languages the
# pure-Go parser uses. The module ships one root LICENSE covering its own code, so
# the upstream grammar copyrights are recorded here. Commits come from the module's
# grammars/languages.lock; copyright lines from each repository's LICENSE at that
# commit. Update both when the gotreesitter version changes.
GRAMMARS = [
    ("go", "tree-sitter/tree-sitter-go", "2346a3ab1bb3857b48b29d779a1ef9799a248cd7", "Copyright (c) 2014 Max Brunsfeld"),
    ("javascript", "tree-sitter/tree-sitter-javascript", "58404d8cf191d69f2674a8fd507bd5776f46cb11", "Copyright (c) 2014 Max Brunsfeld"),
    ("typescript", "tree-sitter/tree-sitter-typescript", "75b3874edb2dc714fb1fd77a32013d0f8699989f", "Copyright (c) 2017 Max Brunsfeld"),
    ("python", "tree-sitter/tree-sitter-python", "26855eabccb19c6abf499fbc5b8dc7cc9ab8bc64", "Copyright (c) 2016 Max Brunsfeld"),
    ("rust", "tree-sitter/tree-sitter-rust", "77a3747266f4d621d0757825e6b11edcbf991ca5", "Copyright (c) 2017 Maxim Sokolov"),
    ("java", "tree-sitter/tree-sitter-java", "e10607b45ff745f5f876bfa3e94fbcc6b44bdc11", "Copyright (c) 2017 Ayman Nadeem"),
    ("c", "tree-sitter/tree-sitter-c", "ae19b676b13bdcc13b7665397e6d9b14975473dd", "Copyright (c) 2014 Max Brunsfeld"),
    ("cpp", "tree-sitter/tree-sitter-cpp", "8b5b49eb196bec7040441bee33b2c9a4838d6967", "Copyright (c) 2014 Max Brunsfeld"),
]
GRAMMAR_MODULE = "github.com/odvcencio/gotreesitter"


# Licences that would make an MIT release misleading. A new dependency carrying
# one of these fails the lint job rather than being noticed later.
COPYLEFT = {"MPL-2.0", "GPL", "LGPL", "AGPL"}


def main():
    mods = modules()
    if not mods:
        sys.exit("go list returned no modules")

    texts = {}   # digest -> (text, licence name, [modules])
    rows = []
    missing = []
    vendor_notices = []

    for path, version, directory in mods:
        lf = licence_file(directory)
        if lf is None:
            missing.append(f"{path} {version}")
            rows.append((path, version, "no licence file found"))
            continue
        text = open(lf, encoding="utf-8", errors="replace").read().strip()
        name = classify(text)
        digest = hashlib.sha256(text.encode()).hexdigest()
        texts.setdefault(digest, [text, name, []])[2].append(f"{path} {version}")
        rows.append((path, version, name))

        nf = notice_file(directory)
        if nf:
            vendor_notices.append(
                (path, open(nf, encoding="utf-8", errors="replace").read().strip()))

    w = sys.stdout.write
    w(f"{PROJECT}\n{COPYRIGHT}\n\n")
    w("Licensed under the MIT License. See LICENSE for the full text.\n\n")
    w("-" * 70 + "\n\n")
    w("Third-party software\n\n")
    w("This binary links the Go modules below. Regenerate this file with\n")
    w("scripts/gen_notice.py after changing dependencies.\n\n")
    width = max(len(p) for p, _, _ in rows) + 2
    for path, version, name in rows:
        w(f"  {path.ljust(width)}{version}  ({name})\n")
    if missing:
        w("\nModules with no licence file in the module cache:\n")
        for m in missing:
            w(f"  {m}\n")

    w("\n" + "-" * 70 + "\n\n")
    w("Licence texts\n\n")
    w("Each distinct text appears once, followed by the modules it covers.\n")
    for digest in sorted(texts, key=lambda d: (texts[d][1], texts[d][2][0])):
        text, name, covered = texts[digest]
        w("\n" + "=" * 70 + "\n")
        w(f"{name}, covering:\n")
        for c in sorted(covered):
            w(f"  {c}\n")
        w("=" * 70 + "\n\n")
        w(text + "\n")

    if vendor_notices:
        w("\n" + "-" * 70 + "\n\n")
        w("Notices required by the modules above\n")
        for path, text in sorted(vendor_notices):
            w("\n" + "=" * 70 + f"\n{path}\n" + "=" * 70 + "\n\n")
            w(text + "\n")

    if any(p == GRAMMAR_MODULE for p, _, _ in rows):
        w("\n" + "-" * 70 + "\n\n")
        w(f"Grammars bundled by {GRAMMAR_MODULE}\n\n")
        w("Each grammar below is MIT licensed by its authors. The MIT licence text\n")
        w("above applies, with these copyright notices:\n\n")
        for lang, repo, commit, holder in GRAMMARS:
            w(f"  {lang}: https://github.com/{repo} at {commit}\n")
            w(f"    {holder}\n")

    copyleft = [f"{p} {v} ({n})" for p, v, n in rows if n in COPYLEFT]
    if copyleft:
        sys.stderr.write(
            "copyleft dependency found, which an MIT release cannot absorb:\n")
        for c in copyleft:
            sys.stderr.write(f"  {c}\n")
        sys.exit(2)


if __name__ == "__main__":
    main()
