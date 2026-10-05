param([string]$InstallDirectory = "", [switch]$Replace)
$ErrorActionPreference = 'Stop'
if (-not $InstallDirectory) {
  if ($env:ELEPHANT_INSTALL_DIR) { $InstallDirectory = $env:ELEPHANT_INSTALL_DIR }
  else { $InstallDirectory = Join-Path $env:LOCALAPPDATA 'Elephant\bin' }
}
# SHA-256 through .NET, so the installer does not depend on the Microsoft.PowerShell.Utility module.
function Get-Sha256Hex([string]$Path) {
  $stream = [System.IO.File]::OpenRead($Path)
  try {
    $hash = [System.Security.Cryptography.SHA256]::Create().ComputeHash($stream)
    return ([System.BitConverter]::ToString($hash) -replace '-', '').ToLowerInvariant()
  } finally { $stream.Dispose() }
}
function Write-UpdateDisclosure([string]$Binary) {
  Write-Output "Elephant checks GitHub Releases for updates at most once per 24h (sends installed version; GitHub sees IP). Opt out with ELEPHANT_UPDATE_CHECKS=0 for this process, or run 'elephant update --enabled=false' to persist."
  if ($env:ELEPHANT_UPDATE_CHECKS -ne '0') { return }
  $optRoot = Join-Path ([System.IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString())
  New-Item -ItemType Directory -Path $optRoot | Out-Null
  $saved = $env:ELEPHANT_UPDATE_CHECKS
  try {
    Remove-Item Env:ELEPHANT_UPDATE_CHECKS -ErrorAction SilentlyContinue
    $PSNativeCommandUseErrorActionPreference = $false
    & $Binary update --enabled=false --root $optRoot
    if ($LASTEXITCODE -ne 0) {
      Write-Output "Could not persist the update opt-out. Run: elephant update --enabled=false"
    }
  } catch {
    Write-Output "Could not persist the update opt-out. Run: elephant update --enabled=false"
  } finally {
    if ($null -ne $saved) { $env:ELEPHANT_UPDATE_CHECKS = $saved }
    Remove-Item -Recurse -Force $optRoot -ErrorAction SilentlyContinue
  }
}
foreach ($line in Get-Content (Join-Path $PSScriptRoot 'SHA256SUMS')) {
  if (-not $line.Trim()) { continue }
  $parts = $line -split '\s+', 2
  $path = Join-Path $PSScriptRoot $parts[1]
  if ((Get-Sha256Hex $path) -ne $parts[0]) {
    throw "Checksum mismatch: $path"
  }
}
New-Item -ItemType Directory -Force -Path $InstallDirectory | Out-Null
$source = Join-Path $PSScriptRoot 'elephant.exe'
$target = Join-Path $InstallDirectory 'elephant.exe'
if (Test-Path $target) {
  if ((Get-Sha256Hex $source) -eq (Get-Sha256Hex $target)) {
    Write-Output "Already installed: $target"
    Write-UpdateDisclosure $target
    exit 0
  }
  if (-not $Replace) { throw "Existing $target preserved. Use -Replace to replace it." }
}
$stage = Join-Path $InstallDirectory ('.elephant-install-' + [guid]::NewGuid().ToString() + '.exe')
try { Copy-Item $source $stage; Move-Item -Force $stage $target }
finally { if (Test-Path $stage) { Remove-Item $stage } }
Write-Output "Installed: $target"
Write-Output "Run: & `"$target`" selftest"
Write-Output 'Add the install directory to PATH if needed; no shell configuration was changed.'
Write-UpdateDisclosure $target
