"""Verify archive versions, checksums/docs and native package execution."""
import argparse
import hashlib
import pathlib
import platform
import posixpath
import os
import re
import subprocess
import tempfile
import zipfile
from release_version import ROOT, VERSION

def verify_archive(archive):
    with zipfile.ZipFile(archive) as z:
        folder = archive.stem
        assert z.read(folder + '/VERSION').decode().strip() == VERSION
        sums = z.read(folder + '/SHA256SUMS').decode().splitlines()
        for row in sums:
            checksum, name = row.split('  ', 1)
            assert hashlib.sha256(z.read(folder + '/' + name)).hexdigest() == checksum
        for name in ['README.md', 'docs/automation.md', 'docs/agent-integration.md', 'docs/updates.md', 'docs/storage.md', f'docs/releases/v{VERSION}.md', 'version.iss']:
            assert folder + '/' + name in z.namelist(), name
        for name in [n for n in z.namelist() if n.endswith('.md')]:
            for target in re.findall(r'\]\(([^)]+)\)', z.read(name).decode()):
                if re.match(r'[a-zA-Z][a-zA-Z0-9+.-]*:', target) or target.startswith('#'): continue
                resolved = posixpath.normpath(posixpath.join(posixpath.dirname(name), target.split('#', 1)[0]))
                assert resolved.startswith(folder + '/') and resolved in z.namelist(), (name, target)
        return folder

def native_target():
    system = platform.system().lower()
    machine = platform.machine().lower()
    arch = {'arm64': 'arm64', 'aarch64': 'arm64', 'x86_64': 'amd64', 'amd64': 'amd64'}.get(machine)
    goos = {'darwin': 'darwin', 'linux': 'linux', 'windows': 'windows'}.get(system)
    if not goos or not arch:
        raise SystemExit(f'unsupported native target {system}/{machine}')
    return goos, arch

def run_native(archive):
    goos, _arch = native_target()
    folder = verify_archive(archive)
    with zipfile.ZipFile(archive) as z, tempfile.TemporaryDirectory(prefix='elephant-package-native-') as tmp:
        z.extractall(tmp)
        extracted = pathlib.Path(tmp) / folder
        binary_name = 'elephant.exe' if goos == 'windows' else 'elephant'
        binary = extracted / binary_name
        if goos != 'windows':
            binary.chmod(0o755)
        subprocess.run([str(binary), 'selftest'], check=True)
        install_dir = pathlib.Path(tmp) / 'installed'
        home = pathlib.Path(tmp) / 'home'
        localapp = pathlib.Path(tmp) / 'localappdata'
        home.mkdir()
        localapp.mkdir()
        env = os.environ.copy()
        env['ELEPHANT_INSTALL_DIR'] = str(install_dir)
        if goos != 'windows':
            # The Windows run only redirects the install directory; installer file hashing
            # does not depend on profile variables.
            env['HOME'] = str(home)
            env['USERPROFILE'] = str(home)
            env['LOCALAPPDATA'] = str(localapp)
        if goos == 'windows':
            subprocess.run(['powershell', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', str(extracted / 'install.ps1')], env=env, check=True)
            installed = install_dir / 'elephant.exe'
        else:
            subprocess.run(['sh', str(extracted / 'install.sh')], env=env, check=True)
            installed = install_dir / 'elephant'
        subprocess.run([str(installed), 'selftest'], env=env, check=True)
    print(f'Native {goos} package checksums, installer, and selftest passed.')

def verify_all():
    found = sorted((ROOT / 'dist').glob(f'elephant-{VERSION}-*.zip'))
    assert len(found) == 6, 'Expected all six platform archives.'
    for archive in found:
        folder = verify_archive(archive)
        if archive.name.endswith('-linux-amd64.zip'):
            with zipfile.ZipFile(archive) as z, tempfile.TemporaryDirectory(prefix='elephant-package-check-') as tmp:
                z.extractall(tmp)
                p = pathlib.Path(tmp) / folder
                (p / 'elephant').chmod(0o755)
                assert subprocess.check_output([str(p/'elephant'), 'version'], text=True).strip() == 'Elephant '+VERSION
                subprocess.run([str(p/'elephant'), 'selftest'], check=True, capture_output=True)
                subprocess.run(['python3',str(ROOT/'scripts/automation_acceptance.py'),str(p/'elephant')],check=True)
                install_dir = pathlib.Path(tmp)/'installed'
                env = dict(os.environ, ELEPHANT_INSTALL_DIR=str(install_dir))
                subprocess.run(['sh', str(p/'install.sh')], env=env, check=True, capture_output=True)
                subprocess.run([str(install_dir/'elephant'), 'selftest'], check=True, capture_output=True)
                subprocess.run(['sh', str(p/'install.sh')], env=env, check=True, capture_output=True)
    print('Six archive versions/checksums/docs and Linux amd64 execution passed.')

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--native', action='store_true', help='execute the archive matching this OS and architecture')
    args = parser.parse_args()
    if args.native:
        goos, arch = native_target()
        archive = ROOT / 'dist' / f'elephant-{VERSION}-{goos}-{arch}.zip'
        if not archive.is_file():
            raise SystemExit(f'missing native archive {archive}')
        run_native(archive)
        return
    verify_all()

if __name__ == '__main__':
    main()
