# Build a per-user Windows x64 MSI. This never installs or launches CoBot.
# MsiVersion is a separate, strictly increasing three-part numeric version:
# Windows Installer cannot compare SemVer prerelease suffixes.
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$MsiVersion,
    [string]$BinaryDirectory,
    [string]$OutputDirectory,
    [string]$WixPath = "wix"
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
if ($env:OS -ne "Windows_NT") { throw "MSI builds require Windows" }
if ($Version -notmatch '^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$') { throw "Invalid product version" }
if ($MsiVersion -notmatch '^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$') { throw "MsiVersion must have three numeric parts" }
$parts = $MsiVersion.Split('.') | ForEach-Object { [long]$_ }
if ($parts[0] -gt 255 -or $parts[1] -gt 255 -or $parts[2] -gt 65535) { throw "MsiVersion exceeds Windows Installer limits" }
if (-not (Get-Command $WixPath -ErrorAction SilentlyContinue)) { throw "WiX CLI missing; provide -WixPath" }
$wixVersion = (& $WixPath --version | Out-String).Trim()
if ($LASTEXITCODE -ne 0 -or $wixVersion -notmatch '^6\.0\.2(?:\+|$)') { throw "This build requires WiX 6.0.2" }
$extensions = & $WixPath extension list -g
if (($extensions | Out-String) -notmatch 'WixToolset\.UI\.wixext 6\.0\.2') {
    throw "WiX UI extension 6.0.2 is missing; install it explicitly before building"
}
Push-Location $root
try {
    if (-not $BinaryDirectory) {
        $BinaryDirectory = Join-Path $root (".tmp/msi/" + $Version + "-" + $MsiVersion + "/bin")
        & "$PSScriptRoot/build.ps1" -Target desktop -Version $Version -OutputDirectory $BinaryDirectory
        if ($LASTEXITCODE -ne 0) { throw "Desktop build failed" }
    }
    $payload = (Resolve-Path -LiteralPath $BinaryDirectory).Path
    foreach ($name in @("cobot-desktop.exe", "cobot-helper.exe", "cobot.exe")) {
        if (-not (Test-Path -LiteralPath (Join-Path $payload $name) -PathType Leaf)) { throw "Missing $name" }
    }
    $actualVersion = (& (Join-Path $payload "cobot.exe") version | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $actualVersion -ne $Version) { throw "Binary version does not match requested version" }
    if (-not $OutputDirectory) { $OutputDirectory = Join-Path $root ".tmp/installer" }
    $out = [IO.Path]::GetFullPath($OutputDirectory)
    New-Item -ItemType Directory -Force -Path $out | Out-Null
    $build = Join-Path $root (".tmp/msi/" + $Version + "-" + $MsiVersion)
    New-Item -ItemType Directory -Force -Path $build | Out-Null
    # A positive allowlist keeps profiles, caches and other local files out.
    $staging = Join-Path $build "payload"
    New-Item -ItemType Directory -Force -Path $staging | Out-Null
    foreach ($name in @("cobot-desktop.exe", "cobot-helper.exe", "cobot.exe")) {
        Copy-Item -LiteralPath (Join-Path $payload $name) -Destination $staging -Force
    }
    Copy-Item -LiteralPath (Join-Path $root "LICENSE") -Destination $staging -Force
    & node scripts/third-party-notices.mjs (Join-Path $staging "THIRD-PARTY-NOTICES.txt")
    if ($LASTEXITCODE -ne 0) { throw "Dependency notices need review" }
    $license = [IO.File]::ReadAllText((Join-Path $root "LICENSE"))
    $rtfText = $license.Replace('\', '\\').Replace('{', '\{').Replace('}', '\}').Replace([string][char]13, '').Replace([string][char]10, '\par ')
    $rtfPath = Join-Path $build "license.rtf"
    [IO.File]::WriteAllText($rtfPath, '{\rtf1\ansi\deff0{\fonttbl{\f0 Segoe UI;}}\f0\fs18 ' + $rtfText + '}')
    $icon = Join-Path $root ".tmp/wails/windows/icon.ico"
    if (-not (Test-Path -LiteralPath $icon -PathType Leaf)) { throw "Build the desktop once to generate its application icon" }
    $msi = Join-Path $out ("CoBot-" + $Version + "-windows-x64.msi")
    $arguments = @("build", "packaging/windows/CoBot.wxs", "-arch", "x64",
        "-ext", "WixToolset.UI.wixext/6.0.2", "-culture", "en-us",
        "-d", "MsiVersion=$MsiVersion", "-d", "Payload=$staging",
        "-d", "AppIcon=$icon", "-d", "LicenseRtf=$rtfPath",
        "-intermediatefolder", (Join-Path $build "obj"), "-pdbtype", "none", "-o", $msi)
    & $WixPath @arguments
    if ($LASTEXITCODE -ne 0) { throw "MSI build failed" }
    $hash = (Get-FileHash -LiteralPath $msi -Algorithm SHA256).Hash.ToLower()
    "$hash  $(Split-Path -Leaf $msi)" | Set-Content -LiteralPath "$msi.sha256" -Encoding ascii
    Write-Host "MSI: $msi"
    Write-Host "Application version: $Version; Windows Installer version: $MsiVersion"
    Write-Host "Unsigned local test package. Installation and upgrade still require validation."
} finally {
    Pop-Location
}
