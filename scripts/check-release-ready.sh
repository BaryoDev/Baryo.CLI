#!/bin/sh
# Says whether a release can actually ship, before a tag makes it permanent.
#
# Pushing a tag is the irreversible step: goreleaser publishes archives, updates the
# Homebrew formula and the Scoop manifest, and a release with a changelog that does not
# describe it cannot be quietly fixed afterwards. Merging feels like finishing, so the
# release bookkeeping is the part that gets forgotten.
#
# What it refuses to call ready:
#   - unassembled fragments still in changelog.d/, so the release notes would be incomplete
#   - no CHANGELOG section for the version being released
#   - an Unreleased section still holding entries that belong in the release
#   - a tag that already exists
#
# It deliberately does not check HomebrewFormula/ or ScoopBucket/: goreleaser writes those
# during the release, so they legitimately lag the tag.
#
#   check-release-ready.sh                        check the newest version in CHANGELOG.md
#   check-release-ready.sh v0.14.0                check a specific version
#   check-release-ready.sh --releasing v0.14.0    same, but the tag is expected to exist
set -eu

cd "$(dirname "$0")/.."

CHANGELOG=${CHANGELOG_FILE:-CHANGELOG.md}
FRAGMENTS=${CHANGELOG_DIR:-changelog.d}

# --releasing is for the release workflow itself, where the tag has already been pushed: the
# "tag must not exist" check is the one thing that cannot hold there, and without this the
# gate would fail every real release and teach everyone to bypass it.
RELEASING=0
if [ "${1:-}" = "--releasing" ]; then
	RELEASING=1
	shift
fi

problems=0
note() {
	echo "  $1"
}
problem() {
	echo "  PROBLEM: $1"
	problems=$((problems + 1))
}

version=${1:-}
if [ -z "$version" ]; then
	# The newest released heading, which is the release being prepared once its section
	# exists. Derived rather than required so the common case needs no argument.
	version=$(grep -m1 '^## v' "$CHANGELOG" 2>/dev/null | sed 's/^## \(v[0-9][^ ]*\).*/\1/' || true)
	if [ -z "$version" ]; then
		problem "$CHANGELOG has no '## v<version>' heading, so there is no release to check"
		echo
		echo "Not ready."
		exit 1
	fi
	note "No version given; checking the newest in $CHANGELOG: $version"
fi

# Accept v0.14.0 or 0.14.0 and normalise, because the tag carries the v and the changelog
# heading and packaging files do not always agree about it.
case "$version" in
v*) tag="$version" ;;
*) tag="v$version" ;;
esac
bare=$(printf '%s' "$tag" | sed 's/^v//')

echo "Checking release readiness for $tag"

# 1. Fragments must be folded in. A fragment left behind is an entry missing from the notes.
pending=0
if [ -d "$FRAGMENTS" ]; then
	for f in "$FRAGMENTS"/*.md; do
		[ -e "$f" ] || continue
		[ "$(basename "$f")" = "README.md" ] && continue
		pending=$((pending + 1))
	done
fi
if [ "$pending" -gt 0 ]; then
	problem "$pending unassembled fragment(s) in $FRAGMENTS — run: bash scripts/changelog-assemble.sh"
else
	note "ok   no unassembled changelog fragments"
fi

# 2. The changelog has to describe this version.
if grep -q "^## $tag\b" "$CHANGELOG" || grep -q "^## $bare\b" "$CHANGELOG"; then
	note "ok   $CHANGELOG has a section for $tag"
else
	problem "$CHANGELOG has no '## $tag' section, so the release would ship undocumented"
fi

# 3. Unreleased must be empty: anything still in it belongs either in this release or in a
#    fragment, and shipping with it populated means the entries never reach a version.
unreleased_entries=$(awk '/^## Unreleased$/{f=1;next} /^## /{f=0} f' "$CHANGELOG" 2>/dev/null | grep -c '^- ' || true)
if [ "${unreleased_entries:-0}" -gt 0 ]; then
	problem "$unreleased_entries entr(ies) still under '## Unreleased' — move them into the $tag section"
else
	note "ok   '## Unreleased' is empty"
fi

# 4. The tag must not exist yet, locally or on the remote. Re-tagging a published release is
#    the one mistake with no clean recovery.
if [ "$RELEASING" = 1 ]; then
	note "skip tag existence check (--releasing: $tag is the release in progress)"
elif git rev-parse -q --verify "refs/tags/$tag" >/dev/null 2>&1; then
	problem "tag $tag already exists locally"
elif git ls-remote --exit-code --tags origin "refs/tags/$tag" >/dev/null 2>&1; then
	problem "tag $tag already exists on origin"
else
	note "ok   tag $tag does not exist yet"
fi

echo
if [ "$problems" -gt 0 ]; then
	echo "Not ready: $problems problem(s) above."
	exit 1
fi
echo "Ready to release $tag."
