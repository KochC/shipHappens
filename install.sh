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
  if command -v gh >/dev/null 2>&1; then
    VERSION="$(gh release view --repo "$REPO" --json tagName -q .tagName 2>/dev/null || true)"
  else
    api="https://api.github.com/repos/${REPO}/releases/latest"
    auth=(); [ -n "${GITHUB_TOKEN:-}" ] && auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
    VERSION="$(curl -fsSL "${auth[@]}" "$api" 2>/dev/null \
      | grep -o '"tag_name": *"[^"]*"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"
  fi
  if [ -z "$VERSION" ]; then
    echo "could not resolve latest version (private repo without gh/GITHUB_TOKEN, or no releases)" >&2
    exit 1
  fi
fi

asset="ship_${VERSION}_${os}_${arch}"
url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"

echo "Installing ship ${VERSION} (${os}/${arch})…"

tmp="$(mktemp)"
# Private repos: release assets require auth. Prefer `gh` when available, else
# use $GITHUB_TOKEN, else try an unauthenticated download (works for public repos).
if command -v gh >/dev/null 2>&1; then
  if ! gh release download "$VERSION" --repo "$REPO" --pattern "$asset" --output "$tmp" --clobber 2>/dev/null; then
    echo "gh download failed" >&2; exit 1
  fi
elif [ -n "${GITHUB_TOKEN:-}" ]; then
  if ! curl -fsSL -H "Authorization: Bearer ${GITHUB_TOKEN}" -o "$tmp" "$url"; then
    echo "authenticated download failed: $url" >&2; exit 1
  fi
else
  if ! curl -fsSL -o "$tmp" "$url"; then
    echo "download failed: $url" >&2
    echo "(private repo? install the GitHub CLI \`gh\` or set GITHUB_TOKEN)" >&2
    exit 1
  fi
fi
chmod +x "$tmp"

# ── choose install dir ──────────────────────────────────────────────────────
bindir="${BINDIR:-/usr/local/bin}"
mkdir -p "$bindir" 2>/dev/null || true
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
echo "For agents/IDEs, the MCP server ships as a separate 'ship-mcp' release asset."
