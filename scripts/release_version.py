"""One package version source: the version reported by the Go binary."""
import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parent.parent
match = re.search(r'^const Version = "([0-9]+\.[0-9]+\.[0-9]+(?:-[a-z0-9.]+)?)"$',
                  (ROOT / 'onboarding.go').read_text(), re.M)
if not match:
    raise RuntimeError('Cannot read the Elephant binary version.')
VERSION = match.group(1)
NUMERIC_VERSION = VERSION.split('-', 1)[0]
DEBIAN_VERSION = VERSION.replace('-', '~', 1)

if __name__ == '__main__':
    print(VERSION)
