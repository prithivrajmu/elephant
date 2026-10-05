#!/usr/bin/env python3
"""Build portable, reproducible release archives; native execution is a separate acceptance gate."""
import hashlib, os, pathlib, shutil, subprocess, time, zipfile
from release_version import VERSION, NUMERIC_VERSION
ROOT = pathlib.Path(__file__).resolve().parent.parent
GO = os.environ.get('ELEPHANT_GO', 'go')
OUT = ROOT / 'dist'
OUT.mkdir(exist_ok=True)
DOCS = ['README.md', 'LICENSE', 'SECURITY.md', 'cloudflare/README.md'] + sorted(p.relative_to(ROOT).as_posix() for p in (ROOT/'docs').rglob('*.md'))
TARGETS = [('darwin','arm64'),('darwin','amd64'),('linux','amd64'),('linux','arm64'),('windows','amd64'),('windows','arm64')]
EXECUTABLES = {'elephant', 'elephant.exe', 'install.sh', 'install.ps1', 'Setup.command', 'Setup.ps1'}

def source_date_epoch():
    raw = os.environ.get('SOURCE_DATE_EPOCH', '').strip()
    if raw:
        return int(raw)
    stamp = subprocess.check_output(['git', 'log', '-1', '--format=%ct'], cwd=ROOT, text=True).strip()
    if not stamp:
        raise RuntimeError('SOURCE_DATE_EPOCH is unset and git has no commit timestamp.')
    return int(stamp)

def zip_timestamp(epoch):
    moment = time.gmtime(epoch)
    return (moment.tm_year, moment.tm_mon, moment.tm_mday, moment.tm_hour, moment.tm_min, moment.tm_sec)

def zip_attr(name):
    mode = 0o755 if pathlib.PurePosixPath(name).name in EXECUTABLES else 0o644
    # Unix regular-file type plus mode, stored in the high 16 bits.
    return (0o100000 | mode) << 16

EPOCH = source_date_epoch()
STAMP = zip_timestamp(EPOCH)
for goos,arch in TARGETS:
    name=f'elephant-{VERSION}-{goos}-{arch}'
    folder=OUT/name
    if folder.exists(): shutil.rmtree(folder)
    folder.mkdir()
    binary='elephant.exe' if goos=='windows' else 'elephant'
    env=dict(os.environ, GOOS=goos, GOARCH=arch, CGO_ENABLED='0', SOURCE_DATE_EPOCH=str(EPOCH))
    subprocess.run([GO,'build','-buildvcs=false','-trimpath','-ldflags=-s -w -buildid=','-o',str(folder/binary),'./cmd/elephant'],cwd=ROOT,env=env,check=True)
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
    files=sorted((item for item in folder.rglob('*') if item.is_file()), key=lambda item: item.relative_to(folder).as_posix())
    checks=''.join(f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.relative_to(folder).as_posix()}\n' for p in files if p.name!='SHA256SUMS')
    (folder/'SHA256SUMS').write_text(checks)
    archive=OUT/(name+'.zip')
    staged_archive=archive.with_suffix('.zip.tmp')
    with zipfile.ZipFile(staged_archive,'w') as z:
        for p in sorted((item for item in folder.rglob('*') if item.is_file()), key=lambda item: item.relative_to(folder).as_posix()):
            rel=p.relative_to(folder).as_posix()
            info=zipfile.ZipInfo(filename=name+'/'+rel, date_time=STAMP)
            info.compress_type=zipfile.ZIP_DEFLATED
            info.create_system=3
            info.external_attr=zip_attr(rel)
            z.writestr(info, p.read_bytes(), compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
    staged_archive.replace(archive)
    print(archive.name,flush=True)
archives=sorted(OUT.glob(f'elephant-{VERSION}-*.zip'))
(OUT/'SHA256SUMS').write_text(''.join(f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n' for p in archives))
