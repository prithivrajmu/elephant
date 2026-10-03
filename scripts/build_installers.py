#!/usr/bin/env python3
"""Build native packages from existing release folders. No uploads/signing performed."""
import argparse, pathlib, platform, shutil, subprocess, tempfile
ROOT=pathlib.Path(__file__).resolve().parent.parent
VERSION='0.4.0-pilot'
p=argparse.ArgumentParser();p.add_argument('--format',choices=['deb','pkg'],required=True);p.add_argument('--arch',choices=['amd64','arm64'],required=True);args=p.parse_args()
goos='linux' if args.format=='deb' else 'darwin'
release=ROOT/'dist'/f'elephant-{VERSION}-{goos}-{args.arch}'
if not (release/'elephant').is_file():raise SystemExit('Build the release archives first.')
with tempfile.TemporaryDirectory(prefix='elephant-native-package-') as tmp:
 stage=pathlib.Path(tmp)
 if args.format=='deb':
  binary=stage/'usr/bin/elephant';binary.parent.mkdir(parents=True);shutil.copy2(release/'elephant',binary);binary.chmod(0o755)
  docs=stage/'usr/share/doc/elephant';docs.mkdir(parents=True)
  for name in ['LICENSE','QUICKSTART.md','LANGUAGE_POLICY.md']:shutil.copy2(ROOT/name,docs/name)
  control=stage/'DEBIAN';control.mkdir()
  (control/'control').write_text(f'Package: elephant\nVersion: 0.4.0~pilot\nSection: utils\nPriority: optional\nArchitecture: {args.arch}\nMaintainer: Elephant contributors\nDescription: Persistent experience for coding agents\n A local memory store, Recall engine, MCP server and Memory Palace.\n')
  subprocess.run(['dpkg-deb','--build','--root-owner-group',str(stage),str(ROOT/'dist'/f'elephant-{VERSION}-linux-{args.arch}.deb')],check=True)
 else:
  if platform.system()!='Darwin':raise SystemExit('Build .pkg on macOS with pkgbuild. No Mac installer was built here.')
  binary=stage/'usr/local/bin/elephant';binary.parent.mkdir(parents=True);shutil.copy2(release/'elephant',binary);binary.chmod(0o755)
  subprocess.run(['pkgbuild','--root',str(stage),'--identifier','dev.elephant.experience','--version','0.4.0','--install-location','/',str(ROOT/'dist'/f'elephant-{VERSION}-darwin-{args.arch}.pkg')],check=True)
