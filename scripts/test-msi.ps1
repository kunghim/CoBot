# Read-only MSI inspection and payload extraction. Never installs a product.
param(
    [Parameter(Mandatory = $true)][string]$Path,
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$MsiVersion,
    [string]$WixPath = "wix"
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$msi = (Resolve-Path -LiteralPath $Path).Path
$work = Join-Path $root (".tmp/msi-verification/" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $work -Force | Out-Null
$installer = New-Object -ComObject WindowsInstaller.Installer
$database = $installer.OpenDatabase($msi, 0)
$checks = [Collections.Generic.List[string]]::new()
function Assert-Msi([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
    $checks.Add($Message)
}
function Read-MsiTable([string]$Table, [string[]]$Columns) {
    $quote = [string][char]96
    $selection = ($Columns | ForEach-Object { $quote + $_ + $quote }) -join ","
    $view = $database.OpenView("SELECT $selection FROM $quote$Table$quote")
    try {
        [void]$view.Execute()
        while ($record = $view.Fetch()) {
            $row = [ordered]@{}
            for ($i = 0; $i -lt $Columns.Count; $i++) { $row[$Columns[$i]] = $record.StringData($i + 1) }
            [pscustomobject]$row
            [void][Runtime.InteropServices.Marshal]::ReleaseComObject($record)
        }
    } finally {
        [void]$view.Close()
        [void][Runtime.InteropServices.Marshal]::ReleaseComObject($view)
    }
}
try {
    $properties = @{}
    Read-MsiTable "Property" @("Property", "Value") | ForEach-Object { $properties[$_.Property] = $_.Value }
    Assert-Msi ($properties.ProductName -eq "CoBot") "Product name is CoBot"
    Assert-Msi ($properties.ProductVersion -eq $MsiVersion) "Numeric MSI version matches"
    Assert-Msi ($properties.UpgradeCode -eq "{D159E043-D132-4929-BD91-423E8B2769E9}") "Upgrade identity is stable"
    Assert-Msi (-not $properties.ALLUSERS) "Installation scope is per-user"
    Assert-Msi ($database.SummaryInformation(0).Property(7) -eq "x64;1033") "Installer targets x64"
    Assert-Msi ($properties.MSIRESTARTMANAGERCONTROL -eq "Disable") "Installer cannot use Restart Manager to close running work"
    Assert-Msi ($properties.REBOOT -eq "ReallySuppress") "Installer does not force a reboot"
    $directories = @(Read-MsiTable "Directory" @("Directory", "Directory_Parent", "DefaultDir"))
    $location = $directories | Where-Object Directory -eq "INSTALLFOLDER"
    Assert-Msi ($location.Directory_Parent -eq "ProgramsFolder" -and $location.DefaultDir -eq "CoBot") "Application folder is CoBot under Programs"
    Assert-Msi ((@($directories | Where-Object { $_.Directory -eq "ProgramsFolder" -and $_.Directory_Parent -eq "LocalAppDataFolder" })).Count -eq 1) "Program files stay in current-user LocalAppData"
    $files = @(Read-MsiTable "File" @("File", "FileName"))
    $allowed = @("cobot-desktop.exe", "cobot-helper.exe", "cobot.exe", "LICENSE", "THIRD-PARTY-NOTICES.txt")
    $names = @($files | ForEach-Object { ($_.FileName -split '\|')[-1] })
    Assert-Msi ($names.Count -eq 5 -and @($names | Where-Object { $_ -notin $allowed }).Count -eq 0) "Payload contains only the five approved program/license files"
    $registry = @(Read-MsiTable "Registry" @("Root", "Key"))
    Assert-Msi (@($registry | Where-Object { $_.Root -ne "1" -or $_.Key -ne "Software\CoBot\Installer" }).Count -eq 0) "Registry writes are confined to HKCU installer markers"
    $shortcuts = @(Read-MsiTable "Shortcut" @("Name", "Target"))
    Assert-Msi ($shortcuts.Count -eq 1 -and $shortcuts[0].Target -eq "[#DesktopExe]") "Only one application shortcut is installed"
    $remove = @(Read-MsiTable "RemoveFile" @("FileName", "DirProperty", "InstallMode"))
    Assert-Msi (@($remove | Where-Object { $_.FileName -or $_.DirProperty -notin @("CoBotMenuFolder", "INSTALLFOLDER") -or $_.InstallMode -ne "2" }).Count -eq 0) "Uninstall cleanup owns only empty CoBot program/menu folders"
    Assert-Msi (@($directories | Where-Object { $_.DefaultDir -match '\.cobot|engines|credentials' }).Count -eq 0) "Runtime profiles and credentials are not installer resources"
    $launch = @(Read-MsiTable "LaunchCondition" @("Condition"))
    Assert-Msi (@($launch | Where-Object Condition -eq "NOT ALLUSERS").Count -eq 1) "Changing to a machine-wide install is blocked"
    Assert-Msi (@($launch | Where-Object Condition -eq "Installed OR NOT COBOT_SAME_VERSION").Count -eq 1) "Different packages with equal MSI versions cannot coexist"
    Assert-Msi (@($launch | Where-Object { $_.Condition -match 'COBOT_WINDOWS_BUILD >= 10240' }).Count -eq 1) "Windows 10 minimum uses the real OS build"
    $sequence = @{}
    Read-MsiTable "InstallExecuteSequence" @("Action", "Sequence") | ForEach-Object { $sequence[$_.Action] = [int]$_.Sequence }
    Assert-Msi ($sequence.RemoveExistingProducts -gt $sequence.InstallInitialize -and $sequence.RemoveExistingProducts -lt $sequence.InstallFinalize) "Major upgrades remove the old product inside the rollback transaction"
    $upgrade = @(Read-MsiTable "Upgrade" @("VersionMin", "VersionMax", "ActionProperty"))
    Assert-Msi (@($upgrade | Where-Object ActionProperty -eq "WIX_DOWNGRADE_DETECTED").Count -eq 1) "Downgrades are detected"
    Assert-Msi (@($upgrade | Where-Object ActionProperty -eq "COBOT_SAME_VERSION").Count -eq 1) "Equal-version packages are detected"
} finally {
    [void][Runtime.InteropServices.Marshal]::ReleaseComObject($database)
    [void][Runtime.InteropServices.Marshal]::ReleaseComObject($installer)
}
$expectedHash = ((Get-Content -LiteralPath "$msi.sha256" -Raw).Trim() -split '\s+')[0]
Assert-Msi ((Get-FileHash -LiteralPath $msi -Algorithm SHA256).Hash.ToLower() -eq $expectedHash) "MSI checksum matches"
& $WixPath msi decompile $msi -out (Join-Path $work "package.wxs") -x (Join-Path $work "payload")
if ($LASTEXITCODE -ne 0) { throw "MSI extraction failed" }
$payload = Join-Path $work "payload/File"
foreach ($entry in @(@("DesktopExe", 2), @("HelperExe", 2), @("ConsoleExe", 3))) {
    $bytes = [IO.File]::ReadAllBytes((Join-Path $payload $entry[0]))
    $pe = [BitConverter]::ToInt32($bytes, 0x3c)
    Assert-Msi ([BitConverter]::ToUInt16($bytes, $pe + 4) -eq 0x8664) ($entry[0] + " is x64")
    Assert-Msi ([BitConverter]::ToUInt16($bytes, $pe + 24 + 68) -eq $entry[1]) ($entry[0] + " has the intended GUI/console subsystem")
}
$cli = Join-Path $work "cobot.exe"
Copy-Item -LiteralPath (Join-Path $payload "ConsoleExe") -Destination $cli
$cliVersion = (& $cli version | Out-String).Trim()
Assert-Msi ($LASTEXITCODE -eq 0 -and $cliVersion -eq $Version) "Extracted CLI version matches"
$notices = Get-Content -LiteralPath (Join-Path $payload "NoticesFile") -Raw
Assert-Msi ($notices.Contains("Go standard library") -and $notices.Contains("github.com/wailsapp/wails/v2") -and $notices.Contains("react ") -and $notices.Contains("modernc.org/sqlite")) "Dependency notices include desktop, frontend and storage dependencies"
$result = [ordered]@{ msi = Split-Path -Leaf $msi; version = $Version; msiVersion = $MsiVersion; checks = $checks; installedOnHost = $false }
$result | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $work "result.json") -Encoding utf8
Write-Host "$($checks.Count) MSI checks passed; no product installed."
Write-Host "Result: $(Join-Path $work 'result.json')"
