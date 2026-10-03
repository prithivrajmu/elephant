"""Verify archive versions, checksums/docs and native Linux package execution."""
import hashlib
import pathlib
import re
import subprocess
import tempfile
import zipfile
from release_version import ROOT, VERSION

archives = sorted((ROOT / 'dist').glob(f'elephant-{VERSION}-*.zip'))
assert len(archives) == 6, 'Expected all six platform archives.'
for archive in archives:
    with zipfile.ZipFile(archive) as z:
        folder = archive.stem
        assert z.read(folder + '/VERSION').decode().strip() == VERSION
        sums = z.read(folder + '/SHA256SUMS').decode().splitlines()
        for row in sums:
            checksum, name = row.split('  ', 1)
            assert hashlib.sha256(z.read(folder + '/' + name)).hexdigest() == checksum
        for name in ['README.md', 'AUTOMATION.md', 'AGENT_INTEGRATION.md', 'UPDATES.md', 'version.iss']:
            assert folder + '/' + name in z.namelist(), name
        for name in [n for n in z.namelist() if n.endswith('.md')]:
            for target in re.findall(r'\]\(([^)]+)\)', z.read(name).decode()):
                if re.match(r'https?://', target): continue
                assert folder + '/' + target.split('#', 1)[0] in z.namelist(), (name, target)
        if archive.name.endswith('-linux-amd64.zip'):
            with tempfile.TemporaryDirectory(prefix='elephant-package-check-') as tmp:
                z.extractall(tmp)
                p = pathlib.Path(tmp) / folder
                (p / 'elephant').chmod(0o755)
                assert subprocess.check_output([str(p/'elephant'), 'version'], text=True).strip() == 'Elephant '+VERSION
                subprocess.run([str(p/'elephant'), 'selftest'], check=True, capture_output=True)
                subprocess.run(['python3',str(ROOT/'scripts/automation_acceptance.py'),str(p/'elephant')],check=True)
print('Six archive versions/checksums/docs and Linux amd64 execution passed.')
