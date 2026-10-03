#!/usr/bin/env python3
"""Build portable release archives; native execution is a separate acceptance gate."""
import hashlib, os, pathlib, shutil, subprocess, zipfile
ROOT = pathlib.Path(__file__).resolve().parent.parent
VERSION = '0.4.0-pilot'
GO = os.environ.get('ELEPHANT_GO', 'go')
OUT = ROOT / 'dist'
OUT.mkdir(exist_ok=True)
DOCS = ['README.md', 'QUICKSTART.md', 'CLIENTS.md', 'PILOT_PLAN.md', 'PILOT_FEEDBACK.md', 'VALIDATION.md', 'LANGUAGE_POLICY.md', 'BRAND.md', 'INSTALLERS.md', 'LICENSE']
TARGETS = [('darwin','arm64'),('darwin','amd64'),('linux','amd64'),('linux','arm64'),('windows','amd64'),('windows','arm64')]
for goos,arch in TARGETS:
    name=f'elephant-{VERSION}-{goos}-{arch}'
    folder=OUT/name
    folder.mkdir(exist_ok=True)
    binary='elephant.exe' if goos=='windows' else 'elephant'
    env=dict(os.environ, GOOS=goos, GOARCH=arch, CGO_ENABLED='0')
    subprocess.run([GO,'build','-buildvcs=false','-trimpath','-ldflags=-s -w','-o',str(folder/binary),'./cmd/elephant'],cwd=ROOT,env=env,check=True)
    (folder/binary).chmod(0o755)
    for doc in DOCS: shutil.copy2(ROOT/doc,folder/doc)
    installer='install.ps1' if goos=='windows' else 'install.sh'
    shutil.copy2(ROOT/'scripts'/installer,folder/installer)
    launcher='Setup.ps1' if goos=='windows' else 'Setup.command'
    shutil.copy2(ROOT/'scripts'/launcher,folder/launcher)
    checks=''.join(f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n' for p in sorted(folder.iterdir()) if p.is_file() and p.name!='SHA256SUMS')
    (folder/'SHA256SUMS').write_text(checks)
    archive=OUT/(name+'.zip')
    staged_archive=archive.with_suffix('.zip.tmp')
    with zipfile.ZipFile(staged_archive,'w',zipfile.ZIP_DEFLATED) as z:
        for p in sorted(folder.iterdir()): z.write(p,name+'/'+p.name)
    staged_archive.replace(archive)
    print(archive.name,flush=True)
archives=sorted(OUT.glob('*.zip'))
(OUT/'SHA256SUMS').write_text(''.join(f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n' for p in archives))
