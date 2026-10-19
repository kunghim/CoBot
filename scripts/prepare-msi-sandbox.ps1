# Stage only installer test inputs. Run the resulting .wsb after enabling
# Windows Sandbox; no Windows feature or product is installed by this script.
param(
    [Parameter(Mandatory = $true)][string]$InitialMsi,
    [Parameter(Mandatory = $true)][string]$UpgradeMsi,
    [Parameter(Mandatory = $true)][string]$DuplicateMsi,
    [Parameter(Mandatory = $true)][string]$InitialVersion,
    [Parameter(Mandatory = $true)][string]$UpgradeVersion
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$run = Join-Path $root (".tmp/msi-sandbox/" + [guid]::NewGuid().ToString("N"))
$inputFolder = Join-Path $run "input"
$outputFolder = Join-Path $run "output"
New-Item -ItemType Directory -Force -Path $inputFolder, $outputFolder | Out-Null
Copy-Item -LiteralPath (Resolve-Path -LiteralPath $InitialMsi).Path -Destination (Join-Path $inputFolder "initial.msi")
Copy-Item -LiteralPath (Resolve-Path -LiteralPath $UpgradeMsi).Path -Destination (Join-Path $inputFolder "upgrade.msi")
Copy-Item -LiteralPath (Resolve-Path -LiteralPath $DuplicateMsi).Path -Destination (Join-Path $inputFolder "duplicate.msi")
Copy-Item -LiteralPath (Join-Path $root "tests/installer/windows-sandbox.ps1") -Destination (Join-Path $inputFolder "smoke.ps1")
@{ scope = "CoBot MSI sandbox verification"; initialVersion = $InitialVersion; upgradeVersion = $UpgradeVersion } |
    ConvertTo-Json | Set-Content -LiteralPath (Join-Path $inputFolder "scope.json") -Encoding utf8
$inputXml = [Security.SecurityElement]::Escape($inputFolder)
$outputXml = [Security.SecurityElement]::Escape($outputFolder)
$configuration = @"
<Configuration>
  <Networking>Enable</Networking>
  <ClipboardRedirection>Disable</ClipboardRedirection>
  <PrinterRedirection>Disable</PrinterRedirection>
  <MappedFolders>
    <MappedFolder>
      <HostFolder>$inputXml</HostFolder>
      <SandboxFolder>C:\CoBotMsiInput</SandboxFolder>
      <ReadOnly>true</ReadOnly>
    </MappedFolder>
    <MappedFolder>
      <HostFolder>$outputXml</HostFolder>
      <SandboxFolder>C:\CoBotMsiOutput</SandboxFolder>
      <ReadOnly>false</ReadOnly>
    </MappedFolder>
  </MappedFolders>
  <LogonCommand>
    <Command>powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\CoBotMsiInput\smoke.ps1</Command>
  </LogonCommand>
</Configuration>
"@
$path = Join-Path $run "CoBot-installer-test.wsb"
[IO.File]::WriteAllText($path, $configuration)
Write-Host "Sandbox configuration: $path"
Write-Host "Results: $(Join-Path $outputFolder 'result.json')"
Write-Host "Only staged installer files and the empty result folder are shared."
