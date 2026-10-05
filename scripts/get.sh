#!/bin/sh
# One-line installer: curl -fsSL https://raw.githubusercontent.com/prithivrajmu/elephant/main/scripts/get.sh | sh
# Env: ELEPHANT_VERSION (e.g. v0.8.0-beta; default latest incl. prereleases), ELEPHANT_INSTALL_DIR.
set -eu
repo=prithivrajmu/elephant
case "$(uname -s)" in Darwin) os=darwin;; Linux) os=linux;; *) echo "Unsupported OS; use get.ps1 on Windows." >&2; exit 1;; esac
case "$(uname -m)" in arm64|aarch64) arch=arm64;; x86_64|amd64) arch=amd64;; *) echo "Unsupported CPU: $(uname -m)" >&2; exit 1;; esac
need() { command -v "$1" >/dev/null 2>&1 || { echo "Required tool missing: $1" >&2; exit 1; }; }
need curl; need unzip
tag=${ELEPHANT_VERSION:-}
if [ -z "$tag" ]; then
  tag=$(curl -fsSL "https://api.github.com/repos/$repo/releases?per_page=1" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
  [ -n "$tag" ] || { echo "Could not determine latest release." >&2; exit 1; }
fi
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT HUP INT TERM
base="https://github.com/$repo/releases/download/$tag"
echo "Installing Elephant $tag ($os-$arch)"
curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS"
name=$(grep "$os-$arch\.zip\$" "$tmp/SHA256SUMS" | awk '{print $2}' | head -n1)
[ -n "$name" ] || { echo "No archive for $os-$arch in $tag." >&2; exit 1; }
curl -fsSL "$base/$name" -o "$tmp/$name"
want=$(grep " $name\$" "$tmp/SHA256SUMS" | awk '{print $1}')
if command -v sha256sum >/dev/null 2>&1; then got=$(sha256sum "$tmp/$name" | awk '{print $1}'); else got=$(shasum -a 256 "$tmp/$name" | awk '{print $1}'); fi
[ "$want" = "$got" ] || { echo "Checksum mismatch for $name." >&2; exit 1; }
unzip -q "$tmp/$name" -d "$tmp/pkg"
dir=$(dirname "$(find "$tmp/pkg" -name install.sh | head -n1)")
ELEPHANT_REPLACE=1 sh "$dir/install.sh"
dest=${ELEPHANT_INSTALL_DIR:-$HOME/.local/bin}
case ":$PATH:" in *":$dest:"*) ;; *) echo "Add to PATH: export PATH=\"$dest:\$PATH\"";; esac
echo "Next: elephant selftest   then   elephant setup --wizard"
