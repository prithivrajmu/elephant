#!/usr/bin/env python3
"""Publish verified portable assets after the native CI gates pass."""
import argparse
import hashlib
import json
import os
import pathlib
import subprocess
import tempfile
from release_version import ROOT, VERSION


def checked_assets():
    targets = ['darwin-arm64', 'darwin-amd64', 'linux-amd64', 'linux-arm64', 'windows-amd64', 'windows-arm64']
    expected = {f'elephant-{VERSION}-{target}.zip' for target in targets}
    sums = ROOT / 'dist' / 'SHA256SUMS'
    checksums = {}
    for row in sums.read_text().splitlines():
        digest, name = row.split('  ', 1)
        if name in checksums or pathlib.Path(name).name != name:
            raise RuntimeError('Invalid or duplicate checksum filename.')
        checksums[name] = digest
    if set(checksums) != expected:
        raise RuntimeError('Release checksum inventory must contain all six platform ZIPs.')
    assets = [ROOT / 'dist' / name for name in sorted(expected)] + [sums]
    for asset in assets[:-1]:
        if hashlib.sha256(asset.read_bytes()).hexdigest() != checksums[asset.name]:
            raise RuntimeError(f'Checksum mismatch: {asset.name}')
    notes = ROOT / 'docs' / 'releases' / f'v{VERSION}.md'
    if not notes.is_file() or not notes.read_text().strip():
        raise RuntimeError('Missing release notes.')
    return assets, notes


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--dry-run', action='store_true')
    args = parser.parse_args()
    assets, notes = checked_assets()
    if args.dry_run:
        print(f'Validated v{VERSION}: six ZIPs, SHA256SUMS and release notes.')
        return
    repo = os.environ['GITHUB_REPOSITORY']
    commit = os.environ['GITHUB_SHA']
    tag = f'v{VERSION}'
    view = subprocess.run(['gh', 'release', 'view', tag, '--repo', repo, '--json', 'isDraft,targetCommitish'], text=True, capture_output=True)
    if view.returncode == 0:
        release = json.loads(view.stdout)
        if not release['isDraft']:
            print(f'{tag} is already published; leaving it unchanged.')
            return
        if release['targetCommitish'] != commit:
            raise RuntimeError('Existing draft targets a different commit; refusing to modify it.')
    elif 'release not found' in view.stderr.lower():
        command = ['gh', 'release', 'create', tag, '--repo', repo, '--target', commit, '--draft', '--title', f'Elephant {VERSION}', '--notes-file', str(notes)]
        if '-' in VERSION:
            command.append('--prerelease')
        subprocess.run(command, check=True)
    else:
        raise RuntimeError(f'Cannot inspect release: {view.stderr.strip()}')
    subprocess.run(['gh', 'release', 'upload', tag, '--repo', repo, '--clobber', *map(str, assets)], check=True)
    with tempfile.TemporaryDirectory(prefix='elephant-release-verify-') as tmp:
        subprocess.run(['gh', 'release', 'download', tag, '--repo', repo, '--dir', tmp], check=True)
        downloaded = pathlib.Path(tmp)
        if {p.name for p in downloaded.iterdir()} != {p.name for p in assets}:
            raise RuntimeError('Uploaded release asset inventory differs from the build.')
        for asset in assets:
            if hashlib.sha256((downloaded / asset.name).read_bytes()).digest() != hashlib.sha256(asset.read_bytes()).digest():
                raise RuntimeError(f'Uploaded asset differs from build: {asset.name}')
    subprocess.run(['gh', 'release', 'edit', tag, '--repo', repo, '--draft=false'], check=True)
    print(f'Published {tag} from {commit} with seven verified assets.')


if __name__ == '__main__':
    main()
