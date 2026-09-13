#!/usr/bin/env bash
# Folds changelog.d/ fragments into CHANGELOG.md's Unreleased section.
#
# One file per change means two branches adding two entries never touch the same file.
# Every pull request editing CHANGELOG.md directly makes every other open branch conflict
# on that one file, and the conflict gets resolved by hand at the point of lowest
# attention — which is how conflict markers reach a default branch and pass every other
# gate, because nothing compiles Markdown.
#
#   changelog-assemble.sh           move the fragments into CHANGELOG.md and delete them
#   changelog-assemble.sh --check   validate the fragments and change nothing
set -euo pipefail

cd "$(dirname "$0")/.."

# Overridable so the fixture test can point the whole script at a throwaway copy. A script
# that can only be exercised by editing the real CHANGELOG.md is one nobody tests.
DIR=${CHANGELOG_DIR:-changelog.d}
FILE=${CHANGELOG_FILE:-CHANGELOG.md}
CHECK=0
[ "${1:-}" = "--check" ] && CHECK=1

# The headings CHANGELOG.md already uses, in the order it uses them.
SECTIONS="Breaking Added Changed Removed Fixed Security"

fail() {
	echo "$1" >&2
	exit 1
}

shopt -s nullglob
fragments=("$DIR"/*.md)
shopt -u nullglob

# Filenames carry the section, so a fragment cannot land under a heading nobody chose.
valid=0
# ${a[@]} on an empty array trips set -u on bash 3.2, which is what macOS ships.
for f in ${fragments[@]+"${fragments[@]}"}; do
	base=$(basename "$f")
	[ "$base" = "README.md" ] && continue

	section=$(echo "$base" | awk -F. '{print $(NF-1)}')
	case " $SECTIONS " in
	*" $section "*) ;;
	*) fail "$f: '$section' is not a section. Name it <slug>.<section>.md, section one of: $SECTIONS" ;;
	esac

	[ -s "$f" ] || fail "$f is empty"
	head -1 "$f" | grep -q '^- \*\*' ||
		fail "$f must open with a bolded lead, like '- **What was wrong.** What changed.'"

	valid=$((valid + 1))
done

if [ "$CHECK" = 1 ]; then
	echo "$valid fragment(s), all well formed."
	exit 0
fi

if [ "$valid" -eq 0 ]; then
	echo "No fragments to assemble."
	exit 0
fi

for section in $SECTIONS; do
	shopt -s nullglob
	matching=("$DIR"/*."$section".md)
	shopt -u nullglob
	[ ${#matching[@]} -eq 0 ] && continue

	body=$(cat ${matching[@]+"${matching[@]}"})

	python3 - "$FILE" "$section" "$body" "$SECTIONS" <<'PY'
import sys
path, section, body, sections = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4].split()
s = open(path, encoding='utf-8').read()

# Every search is scoped to the Unreleased section. An unscoped s.index("### Fixed") finds
# the first such heading in the file, which after a release is the one belonging to the
# version that just shipped — so the entries land under an old release and the script still
# reports success.
marker = "\n## Unreleased\n"
if marker not in s:
    raise SystemExit(f"{path} has no '## Unreleased' heading")
start = s.index(marker) + len(marker)
nxt = s.find("\n## ", start)
end = nxt if nxt != -1 else len(s)
unreleased = s[start:end]

heading = f"\n### {section}\n"
if heading in unreleased:
    # Appended at the end of the existing subsection, so ordering within a release follows
    # the order things were merged rather than the order of a directory listing.
    at = unreleased.index(heading) + len(heading)
    nxt_sub = unreleased.find("\n### ", at)
    cut = nxt_sub if nxt_sub != -1 else len(unreleased)
    unreleased = unreleased[:cut].rstrip('\n') + '\n\n' + body.rstrip('\n') + '\n' + unreleased[cut:]
else:
    # A release empties Unreleased, headings included, so the first fragment of the next
    # cycle has to create its own. Placed in SECTIONS order, so Unreleased reads like every
    # released section.
    later = [f"\n### {x}\n" for x in sections[sections.index(section) + 1:]]
    present = [unreleased.index(h) for h in later if h in unreleased]
    at = min(present) if present else len(unreleased.rstrip('\n'))
    block = f"\n### {section}\n\n" + body.rstrip('\n') + '\n'
    unreleased = unreleased[:at].rstrip('\n') + '\n' + block + unreleased[at:]

# One blank line between the Unreleased heading and its first subsection, and one before the
# next release heading, whatever the slice looked like going in.
s = s[:start] + '\n' + unreleased.strip('\n') + '\n' + s[end:]
open(path, 'w', encoding='utf-8').write(s)
PY

	rm -f ${matching[@]+"${matching[@]}"}
	echo "Assembled ${#matching[@]} into $section"
done

echo "Done. Review the diff before committing."
