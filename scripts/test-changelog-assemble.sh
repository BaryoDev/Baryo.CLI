#!/usr/bin/env bash
# Proves the assembler files entries where it claims to.
#
# The failure this exists for is silent: an unscoped search for "### Fixed" finds the first
# such heading in the file, which after a release belongs to the version that just shipped.
# The fragments land under an old release, the script prints "Assembled 1 into Fixed", and
# the new entries are nowhere a reader will look.
#
# Run against fixtures in a temp directory, never the real CHANGELOG.md.
set -euo pipefail

cd "$(dirname "$0")/.."
ASSEMBLE=$(pwd)/scripts/changelog-assemble.sh

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

pass=0
fail=0
ok() {
	echo "  ok   $1"
	pass=$((pass + 1))
}
bad() {
	echo "  FAIL $1"
	echo "       $2"
	fail=$((fail + 1))
}

# A changelog shaped like the real one: an empty Unreleased section above a shipped release
# that has its own ### Fixed. This is the shape that breaks an unscoped search.
fixture() {
	mkdir -p "$tmp/d"
	rm -f "$tmp/d"/*.md
	cat >"$tmp/CHANGELOG.md" <<'EOF'
# Changelog

## Unreleased

## v1.2.0 — Previous release (2026-01-01)

### Fixed

- **An old entry.** It belongs to the release that already shipped.
EOF
}

# 1. A fragment lands under Unreleased, not under the shipped release.
fixture
cat >"$tmp/d/widget.Fixed.md" <<'EOF'
- **The widget retried twice.** The backoff loop double-counted.
EOF
CHANGELOG_DIR="$tmp/d" CHANGELOG_FILE="$tmp/CHANGELOG.md" bash "$ASSEMBLE" >/dev/null

unreleased=$(awk '/^## Unreleased$/{f=1;next} /^## /{f=0} f' "$tmp/CHANGELOG.md")
if echo "$unreleased" | grep -q 'The widget retried twice'; then
	ok "files a fragment under Unreleased"
else
	bad "files a fragment under Unreleased" "entry is not in the Unreleased section"
fi

shipped=$(awk '/^## v1.2.0/{f=1;next} /^## /{f=0} f' "$tmp/CHANGELOG.md")
if echo "$shipped" | grep -q 'The widget retried twice'; then
	bad "does not touch a shipped release" "entry landed under v1.2.0"
else
	ok "does not touch a shipped release"
fi

if echo "$shipped" | grep -q 'An old entry'; then
	ok "leaves the shipped release intact"
else
	bad "leaves the shipped release intact" "the pre-existing entry is gone"
fi

# 2. Fragments are deleted once folded in, so the next release cannot duplicate them.
if [ -f "$tmp/d/widget.Fixed.md" ]; then
	bad "deletes assembled fragments" "the fragment is still there"
else
	ok "deletes assembled fragments"
fi

# 3. Sections are created in order, so Unreleased reads like a released section.
fixture
printf -- '- **Added thing.** Detail.\n' >"$tmp/d/a.Added.md"
printf -- '- **Fixed thing.** Detail.\n' >"$tmp/d/b.Fixed.md"
printf -- '- **Broke thing.** Detail.\n' >"$tmp/d/c.Breaking.md"
CHANGELOG_DIR="$tmp/d" CHANGELOG_FILE="$tmp/CHANGELOG.md" bash "$ASSEMBLE" >/dev/null
order=$(awk '/^## Unreleased$/{f=1;next} /^## v/{f=0} f && /^### /{print $2}' "$tmp/CHANGELOG.md" | tr '\n' ' ')
if [ "$order" = "Breaking Added Fixed " ]; then
	ok "orders sections Breaking, Added, Fixed"
else
	bad "orders sections Breaking, Added, Fixed" "got: $order"
fi

# 4. A second run appends to the subsection rather than creating a duplicate heading.
printf -- '- **Another fix.** Detail.\n' >"$tmp/d/d.Fixed.md"
CHANGELOG_DIR="$tmp/d" CHANGELOG_FILE="$tmp/CHANGELOG.md" bash "$ASSEMBLE" >/dev/null
count=$(awk '/^## Unreleased$/{f=1;next} /^## v/{f=0} f' "$tmp/CHANGELOG.md" | grep -c '^### Fixed$')
if [ "$count" -eq 1 ]; then
	ok "appends to an existing section instead of duplicating the heading"
else
	bad "appends to an existing section instead of duplicating the heading" "found $count '### Fixed' headings"
fi

# 5. --check rejects the mistakes it is there to catch, and changes nothing.
fixture
printf -- '- **Bad section.** Detail.\n' >"$tmp/d/x.Wibble.md"
if CHANGELOG_DIR="$tmp/d" CHANGELOG_FILE="$tmp/CHANGELOG.md" bash "$ASSEMBLE" --check >/dev/null 2>&1; then
	bad "--check rejects an unknown section" "it passed"
else
	ok "--check rejects an unknown section"
fi

fixture
printf -- 'no bolded lead here\n' >"$tmp/d/y.Fixed.md"
if CHANGELOG_DIR="$tmp/d" CHANGELOG_FILE="$tmp/CHANGELOG.md" bash "$ASSEMBLE" --check >/dev/null 2>&1; then
	bad "--check rejects a fragment with no bolded lead" "it passed"
else
	ok "--check rejects a fragment with no bolded lead"
fi

fixture
: >"$tmp/d/z.Fixed.md"
if CHANGELOG_DIR="$tmp/d" CHANGELOG_FILE="$tmp/CHANGELOG.md" bash "$ASSEMBLE" --check >/dev/null 2>&1; then
	bad "--check rejects an empty fragment" "it passed"
else
	ok "--check rejects an empty fragment"
fi

fixture
if ! CHANGELOG_DIR="$tmp/d" CHANGELOG_FILE="$tmp/CHANGELOG.md" bash "$ASSEMBLE" --check >/dev/null 2>&1; then
	bad "--check passes with no fragments" "it failed"
else
	ok "--check passes with no fragments"
fi

# 6. A changelog with no Unreleased heading is an error, not a silent no-op.
fixture
printf '# Changelog\n\n## v1.2.0 — Only a release\n' >"$tmp/CHANGELOG.md"
printf -- '- **Thing.** Detail.\n' >"$tmp/d/e.Fixed.md"
if CHANGELOG_DIR="$tmp/d" CHANGELOG_FILE="$tmp/CHANGELOG.md" bash "$ASSEMBLE" >/dev/null 2>&1; then
	bad "refuses a changelog with no Unreleased heading" "it reported success"
else
	ok "refuses a changelog with no Unreleased heading"
fi

echo
if [ "$fail" -ne 0 ]; then
	echo "$fail check(s) failed."
	exit 1
fi
echo "all $pass changelog assembler checks passed"
