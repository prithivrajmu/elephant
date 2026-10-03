param([string]$InstallDirectory = "$env:LOCALAPPDATA\Elephant\bin", [switch]$Replace)
$ErrorActionPreference = 'Stop'
foreach ($line in Get-Content (Join-Path $PSScriptRoot 'SHA256SUMS')) {
  if (-not $line.Trim()) { continue }
  $parts = $line -split '\s+', 2
  $path = Join-Path $PSScriptRoot $parts[1]
  if ((Get-FileHash $path -Algorithm SHA256).Hash.ToLowerInvariant() -ne $parts[0]) {
    throw "Checksum mismatch: $path"
  }
}
New-Item -ItemType Directory -Force -Path $InstallDirectory | Out-Null
$source = Join-Path $PSScriptRoot 'elephant.exe'
$target = Join-Path $InstallDirectory 'elephant.exe'
if (Test-Path $target) {
  if ((Get-FileHash $source).Hash -eq (Get-FileHash $target).Hash) {
    Write-Output "Already installed: $target"; exit 0
  }
  if (-not $Replace) { throw "Existing $target preserved. Use -Replace to replace it." }
}
$stage = Join-Path $InstallDirectory ('.elephant-install-' + [guid]::NewGuid().ToString() + '.exe')
try { Copy-Item $source $stage; Move-Item -Force $stage $target }
finally { if (Test-Path $stage) { Remove-Item $stage } }
Write-Output "Installed: $target"
Write-Output "Run: & `"$target`" selftest"
Write-Output 'Add the install directory to PATH if needed; no shell configuration was changed.'
