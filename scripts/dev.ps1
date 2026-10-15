# Shared developer orchestration owns only the resources it starts.
param([string]$DataRoot)
$ErrorActionPreference = 'Stop'
$devArgs = @((Join-Path $PSScriptRoot 'dev.mjs'))
if ($DataRoot) { $devArgs += @('--data-root', $DataRoot) }
node @devArgs
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
