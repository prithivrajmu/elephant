#!/bin/sh
set -eu
package_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$package_dir"
sh ./install.sh
installed_binary="${ELEPHANT_INSTALL_DIR:-$HOME/.local/bin}/elephant"
"$installed_binary" selftest
if [ -n "${ELEPHANT_STORE:-}" ]; then
  "$installed_binary" setup --wizard --store "$ELEPHANT_STORE"
else
  "$installed_binary" setup --wizard
fi
printf '\nSetup finished.\n'
