#!/usr/bin/env python3
"""Build portable release archives; native execution is a separate acceptance gate."""
import hashlib, os, pathlib, shutil, subprocess, zipfile
from release_version import VERSION, NUMERIC_VERSION
ROOT = pathlib.Path(__file__).resolve().parent.parent
GO = os.environ.get('ELEPHANT_GO', 'go')
OUT = ROOT / 'dist'
OUT.mkdir(exist_ok=True)
DOCS = ['README.md', 'LICENSE'] + sorted(p.relative_to(ROOT).as_posix() for p in (ROOT/'docs').rglob('*.md'))
TARGETS = [('darwin','arm64'),('darwin','amd64'),('linux','amd64'),('linux','arm64'),('windows','amd64'),('windows','arm64')]
for goos,arch in TARGETS:
    name=f'elephant-{VERSION}-{goos}-{arch}'
    folder=OUT/name
    if folder.exists(): shutil.rmtree(folder)
    folder.mkdir()
    binary='elephant.exe' if goos=='windows' else 'elephant'
    env=dict(os.environ, GOOS=goos, GOARCH=arch, CGO_ENABLED='0')
    subprocess.run([GO,'build','-buildvcs=false','-trimpath','-ldflags=-s -w','-o',str(folder/binary),'./cmd/elephant'],cwd=ROOT,env=env,check=True)
    (folder/binary).chmod(0o755)
    for doc in DOCS:
        (folder/doc).parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(ROOT/doc,folder/doc)
    (folder/'VERSION').write_text(VERSION+'\n')
    (folder/'version.iss').write_text(f'#define ElephantVersion "{VERSION}"\n#define ElephantNumericVersion "{NUMERIC_VERSION}"\n')
    installer='install.ps1' if goos=='windows' else 'install.sh'
    shutil.copy2(ROOT/'scripts'/installer,folder/installer)
    launcher='Setup.ps1' if goos=='windows' else 'Setup.command'
    shutil.copy2(ROOT/'scripts'/launcher,folder/launcher)
    checks=''.join(f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.relative_to(folder).as_posix()}\n' for p in sorted(folder.rglob('*')) if p.is_file() and p.name!='SHA256SUMS')
    (folder/'SHA256SUMS').write_text(checks)
    archive=OUT/(name+'.zip')
    staged_archive=archive.with_suffix('.zip.tmp')
    with zipfile.ZipFile(staged_archive,'w',zipfile.ZIP_DEFLATED) as z:
        for p in sorted(folder.rglob('*')):
            if p.is_file(): z.write(p,name+'/'+p.relative_to(folder).as_posix())
    staged_archive.replace(archive)
    print(archive.name,flush=True)
archives=sorted(OUT.glob(f'elephant-{VERSION}-*.zip'))
(OUT/'SHA256SUMS').write_text(''.join(f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n' for p in archives))
