#!/bin/sh
set -eu

REPO="tesserakdev/tsk"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: $1 is required but not installed" >&2
    exit 1
  fi
}
need curl
need tar

OS=$(uname -s)
case "$OS" in
  Darwin) OS="darwin" ;;
  Linux)  OS="linux" ;;
  *)
    echo "error: unsupported OS: $OS" >&2
    exit 1
    ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
  x86_64)         ARCH="amd64" ;;
  arm64|aarch64)  ARCH="arm64" ;;
  *)
    echo "error: unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

printf "Fetching latest release... "
TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' \
  | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')

if [ -z "$TAG" ]; then
  echo "error: failed to fetch latest release" >&2
  exit 1
fi
echo "$TAG"

ARCHIVE="tsk_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "Downloading ${ARCHIVE}..."
curl -fsSL "${BASE_URL}/${ARCHIVE}" -o "${TMP}/${ARCHIVE}"
curl -fsSL "${BASE_URL}/checksums.txt" -o "${TMP}/checksums.txt"

echo "Verifying checksum..."
cd "$TMP"
if command -v sha256sum >/dev/null 2>&1; then
  grep "  ${ARCHIVE}$" checksums.txt | sha256sum -c -
elif command -v shasum >/dev/null 2>&1; then
  grep "  ${ARCHIVE}$" checksums.txt | shasum -a 256 -c -
else
  echo "warning: no sha256 tool found, skipping checksum verification" >&2
fi
cd - >/dev/null

if command -v gh >/dev/null 2>&1; then
  echo "Verifying build provenance..."
  gh attestation verify "${TMP}/${ARCHIVE}" --repo "${REPO}"
else
  echo "warning: gh not found, skipping provenance verification" >&2
fi

tar -xzf "${TMP}/${ARCHIVE}" -C "${TMP}"

DEST="${INSTALL_DIR}/tsk"
if [ -w "$INSTALL_DIR" ]; then
  mv "${TMP}/tsk" "$DEST"
else
  echo "Installing to ${DEST} (sudo required)..."
  sudo mv "${TMP}/tsk" "$DEST"
fi
chmod +x "$DEST"

echo ""
echo "tsk ${TAG} installed to ${DEST}"
echo "Run 'tsk init' to get started."
