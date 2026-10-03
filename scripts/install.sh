#!/bin/sh
set -eu
package_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$package_dir"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum -c SHA256SUMS
elif command -v shasum >/dev/null 2>&1; then
  shasum -a 256 -c SHA256SUMS
else
  echo 'A SHA-256 verifier (sha256sum or shasum) is required.' >&2; exit 1
fi
install_dir=${ELEPHANT_INSTALL_DIR:-"$HOME/.local/bin"}
mkdir -p "$install_dir"
target="$install_dir/elephant"
if [ -e "$target" ]; then
  if cmp -s elephant "$target"; then
    echo "Already installed: $target"; exit 0
  fi
  if [ "${ELEPHANT_REPLACE:-0}" != 1 ]; then
    echo "Existing $target preserved. Set ELEPHANT_REPLACE=1 to replace it." >&2; exit 1
  fi
fi
stage=$(mktemp "$install_dir/.elephant-install.XXXXXX")
trap 'rm -f "$stage"' EXIT HUP INT TERM
cp elephant "$stage"
chmod 755 "$stage"
mv -f "$stage" "$target"
echo "Installed: $target"
echo "Run: \"$target\" selftest"
echo 'Add the install directory to PATH if needed; no shell configuration was changed.'
