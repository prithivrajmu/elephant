# One-line installer: irm https://raw.githubusercontent.com/prithivrajmu/elephant/main/scripts/get.ps1 | iex
# Env: ELEPHANT_VERSION (default latest incl. prereleases), ELEPHANT_INSTALL_DIR.
$ErrorActionPreference = 'Stop'
$repo = 'prithivrajmu/elephant'
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$tag = $env:ELEPHANT_VERSION
if (-not $tag) { $tag = (Invoke-RestMethod "https://api.github.com/repos/$repo/releases?per_page=1")[0].tag_name }
$base = "https://github.com/$repo/releases/download/$tag"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString())
New-Item -ItemType Directory $tmp | Out-Null
try {
  Write-Output "Installing Elephant $tag (windows-$arch)"
  $sums = (Invoke-WebRequest "$base/SHA256SUMS" -UseBasicParsing).Content -split "`n"
  $line = $sums | Where-Object { $_ -match "windows-$arch\.zip\s*$" } | Select-Object -First 1
  if (-not $line) { throw "No archive for windows-$arch in $tag" }
  $want, $name = ($line.Trim() -split '\s+')
  $zip = Join-Path $tmp $name
  Invoke-WebRequest "$base/$name" -OutFile $zip -UseBasicParsing
  $got = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($got -ne $want.ToLowerInvariant()) { throw "Checksum mismatch for $name" }
  Expand-Archive $zip -DestinationPath (Join-Path $tmp 'pkg')
  $installer = Get-ChildItem (Join-Path $tmp 'pkg') -Recurse -Filter install.ps1 | Select-Object -First 1
  & $installer.FullName -Replace
  $dest = if ($env:ELEPHANT_INSTALL_DIR) { $env:ELEPHANT_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Elephant\bin' }
  $user = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (($user -split ';') -notcontains $dest) { [Environment]::SetEnvironmentVariable('Path', "$dest;$user", 'User') }
  $env:Path = "$dest;$env:Path"
  Write-Output 'Next: elephant selftest   then   elephant setup --wizard'
} finally { Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue }
