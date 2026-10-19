# Assemble and inspect release files without installing or launching the desktop.
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$MsiVersion,
    [string]$PreviousMsi
)
$ErrorActionPreference = "Stop"
Set-Location (Split-Path -Parent $PSScriptRoot)
if ($env:OS -ne "Windows_NT") { throw "Windows release packaging requires Windows" }

if ($PreviousMsi) {
    $installer = New-Object -ComObject WindowsInstaller.Installer
    $database = $installer.OpenDatabase((Resolve-Path -LiteralPath $PreviousMsi).Path, 0)
    $quote = [string][char]96
    $query = "SELECT " + $quote + "Value" + $quote + " FROM " + $quote + "Property" + $quote + " WHERE " + $quote + "Property" + $quote + " = 'ProductVersion'"
    $view = $database.OpenView($query)
    try {
        [void]$view.Execute()
        $record = $view.Fetch()
        if (-not $record) { throw "Previous MSI has no ProductVersion" }
        $previousVersion = $record.StringData(1)
        if ([version]$MsiVersion -le [version]$previousVersion) {
            throw "MSI version $MsiVersion must be greater than the published version $previousVersion"
        }
        Write-Host "MSI upgrade order: $previousVersion -> $MsiVersion"
    } finally {
        if ($record) { [void][Runtime.InteropServices.Marshal]::ReleaseComObject($record) }
        [void]$view.Close()
        [void][Runtime.InteropServices.Marshal]::ReleaseComObject($view)
        [void][Runtime.InteropServices.Marshal]::ReleaseComObject($database)
        [void][Runtime.InteropServices.Marshal]::ReleaseComObject($installer)
    }
}

& "$PSScriptRoot/build-msi.ps1" -Version $Version -MsiVersion $MsiVersion
if ($LASTEXITCODE -ne 0) { throw "MSI build failed" }
$msi = Join-Path ".tmp/installer" ("CoBot-" + $Version + "-windows-x64.msi")
& "$PSScriptRoot/test-msi.ps1" -Path $msi -Version $Version -MsiVersion $MsiVersion
if ($LASTEXITCODE -ne 0) { throw "MSI verification failed" }

# Use the same five staged files as the MSI, never a recursive build-directory ZIP.
$payload = Join-Path ".tmp/msi" ($Version + "-" + $MsiVersion + "/payload")
$allowed = @("cobot-desktop.exe", "cobot-helper.exe", "cobot.exe", "LICENSE", "THIRD-PARTY-NOTICES.txt")
$files = @($allowed | ForEach-Object { Join-Path $payload $_ })
$out = ".tmp/release-assets"
New-Item -ItemType Directory -Force -Path $out | Out-Null
$zip = Join-Path $out ("CoBot-" + $Version + "-windows-x64.zip")
Compress-Archive -LiteralPath $files -DestinationPath $zip -Force
$hash = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLower()
"$hash  $(Split-Path -Leaf $zip)" | Set-Content -LiteralPath "$zip.sha256" -Encoding ascii

Add-Type -AssemblyName System.IO.Compression.FileSystem
$archive = [IO.Compression.ZipFile]::OpenRead([IO.Path]::GetFullPath($zip))
try {
    $actual = @($archive.Entries | ForEach-Object FullName | Sort-Object)
    if (($actual -join "|") -ne (($allowed | Sort-Object) -join "|")) { throw "ZIP payload mismatch" }
    foreach ($entry in $archive.Entries) {
        $source = [IO.File]::OpenRead([IO.Path]::GetFullPath((Join-Path $payload $entry.FullName)))
        $packed = $entry.Open()
        try {
            $digest = [Security.Cryptography.SHA256]::Create()
            $sourceHash = [Convert]::ToHexString($digest.ComputeHash($source))
            $packedHash = [Convert]::ToHexString($digest.ComputeHash($packed))
            if ($sourceHash -ne $packedHash) { throw "ZIP content differs from MSI payload: $($entry.FullName)" }
        } finally {
            if ($digest) { $digest.Dispose() }
            $source.Dispose()
            $packed.Dispose()
        }
    }
} finally { $archive.Dispose() }
Copy-Item -LiteralPath $msi, "$msi.sha256" -Destination $out -Force
Write-Host "Windows release files verified in $out; no application installed."
