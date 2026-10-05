#!/usr/bin/env node
'use strict';
// npx elephant-memory [command]
//   (no args)  download + verify the binary, install it to ~/.local/bin, print next steps
//   anything else is passed through to the elephant binary (e.g. `npx elephant-memory init`)
const fs = require('fs');
const os = require('os');
const path = require('path');
const zlib = require('zlib');
const crypto = require('crypto');
const { spawnSync } = require('child_process');

const repo = 'prithivrajmu/elephant';
const version = require('../package.json').version;
const tag = process.env.ELEPHANT_VERSION || `v${version}`;
const plat = { darwin: 'darwin', linux: 'linux', win32: 'windows' }[process.platform];
const arch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
if (!plat || !arch) fail(`Unsupported platform: ${process.platform}-${process.arch}`);
const exe = plat === 'windows' ? 'elephant.exe' : 'elephant';
const cacheDir = path.join(os.homedir(), '.elephant', 'bin', tag);
const cached = path.join(cacheDir, exe);

function fail(msg) { console.error(`elephant-memory: ${msg}`); process.exit(1); }

async function get(url) {
  const res = await fetch(url, { redirect: 'follow' });
  if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`);
  return Buffer.from(await res.arrayBuffer());
}

// Minimal ZIP reader (stored/deflate) so no external unzip tool is needed.
function extract(zip, wanted) {
  let eocd = -1;
  for (let i = zip.length - 22; i >= 0; i--) if (zip.readUInt32LE(i) === 0x06054b50) { eocd = i; break; }
  if (eocd < 0) throw new Error('invalid zip');
  let pos = zip.readUInt32LE(eocd + 16);
  for (let n = zip.readUInt16LE(eocd + 10); n > 0; n--) {
    const method = zip.readUInt16LE(pos + 10), csize = zip.readUInt32LE(pos + 20);
    const nlen = zip.readUInt16LE(pos + 28), xlen = zip.readUInt16LE(pos + 30), clen = zip.readUInt16LE(pos + 32);
    const off = zip.readUInt32LE(pos + 42);
    const name = zip.toString('utf8', pos + 46, pos + 46 + nlen);
    pos += 46 + nlen + xlen + clen;
    if (name.split('/').pop() !== wanted) continue;
    const start = off + 30 + zip.readUInt16LE(off + 26) + zip.readUInt16LE(off + 28);
    const data = zip.subarray(start, start + csize);
    return method === 0 ? data : zlib.inflateRawSync(data);
  }
  throw new Error(`${wanted} not found in archive`);
}

async function ensureBinary() {
  if (fs.existsSync(cached)) return cached;
  const base = `https://github.com/${repo}/releases/download/${tag}`;
  const sums = (await get(`${base}/SHA256SUMS`)).toString('utf8').split('\n');
  const line = sums.find((l) => l.trim().endsWith(`${plat}-${arch}.zip`));
  if (!line) fail(`no ${plat}-${arch} archive in release ${tag}`);
  const [want, name] = line.trim().split(/\s+/);
  console.error(`Downloading Elephant ${tag} (${plat}-${arch})...`);
  const zip = await get(`${base}/${name}`);
  const got = crypto.createHash('sha256').update(zip).digest('hex');
  if (got !== want.toLowerCase()) fail(`checksum mismatch for ${name}`);
  fs.mkdirSync(cacheDir, { recursive: true });
  const tmp = `${cached}.${process.pid}.tmp`;
  fs.writeFileSync(tmp, extract(zip, exe), { mode: 0o755 });
  fs.renameSync(tmp, cached);
  return cached;
}

function installToPath(bin, quiet) {
  const dir = process.env.ELEPHANT_INSTALL_DIR ||
    (plat === 'windows' ? path.join(process.env.LOCALAPPDATA || os.homedir(), 'Elephant', 'bin') : path.join(os.homedir(), '.local', 'bin'));
  fs.mkdirSync(dir, { recursive: true });
  const target = path.join(dir, exe);
  fs.copyFileSync(bin, target);
  fs.chmodSync(target, 0o755);
  if (quiet) return target;
  console.log(`Installed: ${target}`);
  if (!(process.env.PATH || '').split(path.delimiter).includes(dir)) {
    console.log(plat === 'windows' ? `Add ${dir} to your PATH.` : `Add to PATH: export PATH="${dir}:$PATH"`);
  }
  return target;
}

(async () => {
  const bin = await ensureBinary();
  const args = process.argv.slice(2);
  if (args.length === 0) {
    installToPath(bin);
    console.log('Next, in your project directory: npx elephant-memory init   (installs Codex/Claude hooks)\nOr for MCP clients: npx elephant-memory setup --wizard');
    return;
  }
  // Run the installed copy so hooks written by `init` record a stable binary path.
  const r = spawnSync(installToPath(bin, true), args, { stdio: 'inherit' });
  process.exit(r.status === null ? 1 : r.status);
})().catch((e) => fail(e.message));
