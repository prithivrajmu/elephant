param([string]$InstallDirectory = "$env:LOCALAPPDATA\Elephant\bin", [switch]$Replace, [string]$Store = "")
$ErrorActionPreference = 'Stop'
& (Join-Path $PSScriptRoot 'install.ps1') -InstallDirectory $InstallDirectory -Replace:$Replace
$binary = Join-Path $InstallDirectory 'elephant.exe'
& $binary selftest
if ($LASTEXITCODE -ne 0) { throw 'Elephant selftest failed.' }
if ($Store) { & $binary setup --wizard --store $Store }
else { & $binary setup --wizard }
if ($LASTEXITCODE -ne 0) { throw 'Elephant setup stopped.' }
