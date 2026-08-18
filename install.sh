#!/usr/bin/env bash
# Install the Ship Happens `ship` CLI from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/KochC/shipHappens/main/install.sh | bash
#   curl -fsSL .../install.sh | VERSION=v0.1.0 BINDIR=~/.local/bin bash
#
# Env:
#   VERSION  release tag to install (default: latest)
#   BINDIR   install directory (default: /usr/local/bin, falls back to ~/.local/bin)
set -euo pipefail

REPO="KochC/shipHappens"
VERSION="${VERSION:-latest}"

# ── detect OS/arch ──────────────────────────────────────────────────────────
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux)  os=linux ;;
  darwin) os=darwin ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

# ── resolve version ─────────────────────────────────────────────────────────
if [ "$VERSION" = "latest" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -o '"tag_name": *"[^"]*"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"
  if [ -z "$VERSION" ]; then
    echo "could not resolve latest version (no releases yet?)" >&2
    exit 1
  fi
fi

asset="ship_${VERSION}_${os}_${arch}"
url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"

echo "Installing ship ${VERSION} (${os}/${arch})…"

tmp="$(mktemp)"
if ! curl -fsSL -o "$tmp" "$url"; then
  echo "download failed: $url" >&2
  exit 1
fi
chmod +x "$tmp"

# ── choose install dir ──────────────────────────────────────────────────────
bindir="${BINDIR:-/usr/local/bin}"
if [ ! -w "$bindir" ]; then
  bindir="${HOME}/.local/bin"
  mkdir -p "$bindir"
fi
install -m 0755 "$tmp" "${bindir}/ship"
rm -f "$tmp"

echo "Installed to ${bindir}/ship"
case ":$PATH:" in
  *":$bindir:"*) ;;
  *) echo "note: add ${bindir} to your PATH" >&2 ;;
esac
"${bindir}/ship" version || true

echo
echo "Pkl pipelines also require the pkl CLI: https://pkl-lang.org (e.g. brew install pkl)"
