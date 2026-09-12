#!/bin/sh
# Baryo CLI installer
# Usage: curl -fsSL https://raw.githubusercontent.com/BaryoDev/Baryo.CLI/main/install.sh | sh
set -e

REPO="BaryoDev/Baryo.CLI"
BINARY="baryo"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# detect_platform echoes "<os> <arch>" or exits with a message.
detect_platform() {
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)

  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) echo "Error: unsupported architecture $arch" >&2; exit 1 ;;
  esac

  case "$os" in
    linux|darwin) ;;
    *) echo "Error: unsupported OS $os (use Scoop on Windows)" >&2; exit 1 ;;
  esac

  echo "$os $arch"
}

# sha256_of prints the sha256 hex digest of a file. Returns 1 with no tool.
# macOS ships shasum but not sha256sum; most Linux images are the reverse.
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    return 1
  fi
}

# verify_checksum <file> <checksums-file> <name-in-checksums>
#
# checksums.txt comes from the same GitHub release as the tarball, so this
# catches a truncated download, CDN corruption or a single swapped asset. It is
# not a signature and does not detect a compromised release.
verify_checksum() {
  file=$1
  sums=$2
  name=$3

  expected=$(awk -v n="$name" '$2 == n || $2 == "*" n { print $1; exit }' "$sums")
  if [ -z "$expected" ]; then
    echo "Error: no checksum published for ${name}" >&2
    return 1
  fi

  if ! actual=$(sha256_of "$file"); then
    echo "Error: need sha256sum or shasum to verify the download" >&2
    return 1
  fi

  if [ "$expected" != "$actual" ]; then
    echo "Error: checksum mismatch for ${name}" >&2
    echo "  expected ${expected}" >&2
    echo "  actual   ${actual}" >&2
    return 1
  fi
}

main() {
  platform=$(detect_platform)
  os=$(echo "$platform" | cut -d' ' -f1)
  arch=$(echo "$platform" | cut -d' ' -f2)

  echo "Fetching latest release..."
  tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')

  if [ -z "$tag" ]; then
    echo "Error: could not determine latest release" >&2
    exit 1
  fi

  version="${tag#v}"
  filename="${BINARY}_${version}_${os}_${arch}.tar.gz"
  base="https://github.com/${REPO}/releases/download/${tag}"

  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT

  echo "Downloading ${BINARY} ${tag} for ${os}/${arch}..."
  curl -fsSL "${base}/${filename}" -o "${work}/${filename}"

  echo "Verifying checksum..."
  curl -fsSL "${base}/checksums.txt" -o "${work}/checksums.txt"
  verify_checksum "${work}/${filename}" "${work}/checksums.txt" "$filename"

  echo "Extracting..."
  tar -xzf "${work}/${filename}" -C "$work"

  # chmod before the move: after a sudo mv the file is root-owned and this
  # would fail, aborting the script under set -e once installation is done.
  chmod +x "${work}/${BINARY}"

  echo "Installing to ${INSTALL_DIR}/${BINARY}..."
  if [ -w "$INSTALL_DIR" ]; then
    mv "${work}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  else
    sudo mv "${work}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  fi

  echo ""
  echo "Installed ${BINARY} ${tag} to ${INSTALL_DIR}/${BINARY}"
  echo "Run 'baryo doctor' to verify your setup."
}

# Run only when executed, not when sourced by scripts/install_test.sh, and so a
# truncated `curl | sh` cannot execute a partial script.
if [ -z "${BARYO_INSTALL_SOURCED:-}" ]; then
  main "$@"
fi
