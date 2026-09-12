#!/bin/sh
# Tests the checksum verification in install.sh without cutting a release.
# Run: sh scripts/install_test.sh
set -e

here=$(cd "$(dirname "$0")/.." && pwd)
BARYO_INSTALL_SOURCED=1 . "${here}/install.sh"

fails=0
pass() { echo "  ok   $1"; }
fail() { echo "  FAIL $1"; fails=$((fails + 1)); }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

name="baryo_9.9.9_linux_amd64.tar.gz"
printf 'pretend tarball\n' > "${work}/${name}"
sum=$(sha256_of "${work}/${name}")
printf '%s  %s\n' "$sum" "$name" > "${work}/checksums.txt"
printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" "other_file.tar.gz" >> "${work}/checksums.txt"

echo "verify_checksum:"

if verify_checksum "${work}/${name}" "${work}/checksums.txt" "$name" 2>/dev/null; then
  pass "accepts a matching checksum"
else
  fail "accepts a matching checksum"
fi

printf 'tampered\n' > "${work}/${name}"
if verify_checksum "${work}/${name}" "${work}/checksums.txt" "$name" 2>/dev/null; then
  fail "rejects a modified file"
else
  pass "rejects a modified file"
fi

if verify_checksum "${work}/${name}" "${work}/checksums.txt" "absent.tar.gz" 2>/dev/null; then
  fail "rejects a filename absent from checksums.txt"
else
  pass "rejects a filename absent from checksums.txt"
fi

# A prefix of another entry's name must not be accepted as a match.
printf '%s  %s\n' "$sum" "baryo_9.9.9_linux_amd64.tar.gz.sig" > "${work}/only_sig.txt"
if verify_checksum "${work}/${name}" "${work}/only_sig.txt" "$name" 2>/dev/null; then
  fail "does not match a different filename that shares a prefix"
else
  pass "does not match a different filename that shares a prefix"
fi

echo ""
if [ "$fails" -ne 0 ]; then
  echo "${fails} failure(s)"
  exit 1
fi
echo "all install.sh checks passed"
