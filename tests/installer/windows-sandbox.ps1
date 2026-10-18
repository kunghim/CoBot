# Run only inside a fresh Windows Sandbox. No host profile is mapped here.
$ErrorActionPreference = "Stop"
if ($env:USERNAME -ne "WDAGUtilityAccount" -or -not (Test-Path -LiteralPath "C:\CoBotMsiInput\scope.json")) {
    throw "This test is restricted to the generated Windows Sandbox environment"
}
$output = "C:\CoBotMsiOutput"
$result = [ordered]@{ passed = $false; checks = @(); scope = "Windows Sandbox only" }
function Assert-Smoke([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
    $script:result.checks += $Message
}
function Run-Msi([string]$Operation, [string]$Package, [string]$Name, [bool]$ShouldFail = $false) {
    # Windows Installer runs in a service context. Keep both package and log
    # on the guest disk instead of asking the service to use a redirected share.
    $log = Join-Path $testRoot ($Name + ".log")
    $arguments = @($Operation, ('"' + $Package + '"'), "/qn", "/norestart", "/l*v", ('"' + $log + '"'))
    $process = Start-Process -FilePath "$env:WINDIR\System32\msiexec.exe" -ArgumentList $arguments -Wait -PassThru -WindowStyle Hidden
    Copy-Item -LiteralPath $log -Destination (Join-Path $output ($Name + ".log"))
    if ($ShouldFail) {
        Assert-Smoke ($process.ExitCode -eq 1603) ($Name + " refused with the expected installer error")
    } else {
        Assert-Smoke ($process.ExitCode -eq 0) ($Name + " completed without a reboot")
    }
}
function Installed-CoBot {
    # Query MSI registration directly: Add/Remove Programs registry placement
    # alone does not establish the installation context.
    $installer = New-Object -ComObject WindowsInstaller.Installer
    $get = [Reflection.BindingFlags]::GetProperty
    $products = $installer.GetType().InvokeMember("ProductsEx", $get, $null, $installer, @("", "", 7))
    foreach ($product in $products) {
        $name = $product.GetType().InvokeMember("InstallProperty", $get, $null, $product, @("ProductName"))
        if ($name -eq "CoBot") {
            [pscustomobject]@{
                ProductCode = $product.GetType().InvokeMember("ProductCode", $get, $null, $product, @())
                UserSid = $product.GetType().InvokeMember("UserSid", $get, $null, $product, @())
                Context = $product.GetType().InvokeMember("Context", $get, $null, $product, @())
            }
        }
    }
}
try {
    # Refuse accidental use on a maintainer's regular Windows session.
    if ($env:USERNAME -ne "WDAGUtilityAccount" -or -not (Test-Path -LiteralPath "C:\CoBotMsiInput\scope.json")) {
        throw "This test is restricted to the generated Windows Sandbox environment"
    }
    $scope = Get-Content -LiteralPath "C:\CoBotMsiInput\scope.json" -Raw | ConvertFrom-Json
    if ($scope.scope -ne "CoBot MSI sandbox verification") { throw "Invalid test scope" }
    $policy = Get-ItemProperty -LiteralPath "HKLM:\SYSTEM\CurrentControlSet\Control\CI\Policy" -Name VerifiedAndReputablePolicyState -ErrorAction SilentlyContinue
    $result.environment = @{ smartAppControlState = $policy.VerifiedAndReputablePolicyState }
    Assert-Smoke (@(Installed-CoBot).Count -eq 0) "No CoBot installation exists in the fresh guest"
    $install = Join-Path ([Environment]::GetFolderPath("LocalApplicationData")) "Programs\CoBot"
    $menu = Join-Path ([Environment]::GetFolderPath("Programs")) "CoBot\CoBot.lnk"
    $testRoot = Join-Path $env:TEMP ("CoBotMsiTest-" + [guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $testRoot -Force | Out-Null
    foreach ($package in @("initial.msi", "upgrade.msi", "duplicate.msi")) {
        Copy-Item -LiteralPath (Join-Path "C:\CoBotMsiInput" $package) -Destination (Join-Path $testRoot $package)
    }
    # Synthetic preservation marker only; no real credentials are imported.
    $profile = Join-Path $env:USERPROFILE ".cobot"
    Assert-Smoke (-not (Test-Path -LiteralPath $profile)) "Guest has no existing production profile"
    New-Item -ItemType Directory -Path $profile | Out-Null
    $marker = Join-Path $profile "installer-test-marker.txt"
    $markerValue = [guid]::NewGuid().ToString()
    [IO.File]::WriteAllText($marker, $markerValue)
    Run-Msi "/i" (Join-Path $testRoot "initial.msi") "01-install"
    foreach ($name in @("cobot-desktop.exe", "cobot-helper.exe", "cobot.exe", "LICENSE", "THIRD-PARTY-NOTICES.txt")) {
        Assert-Smoke (Test-Path -LiteralPath (Join-Path $install $name)) ("Installed " + $name)
    }
    Assert-Smoke (Test-Path -LiteralPath $menu) "Start menu has one CoBot entry"
    Assert-Smoke ((& (Join-Path $install "cobot.exe") version) -eq $scope.initialVersion) "Initial application version matches"
    $products = @(Installed-CoBot)
    $userSid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    Assert-Smoke ($products.Count -eq 1 -and $products[0].Context -eq 2 -and $products[0].UserSid -eq $userSid) "Initial installation is registered for the current user only"
    Run-Msi "/i" (Join-Path $testRoot "upgrade.msi") "02-upgrade"
    Assert-Smoke ((& (Join-Path $install "cobot.exe") version) -eq $scope.upgradeVersion) "Upgrade replaced the application version"
    $products = @(Installed-CoBot)
    Assert-Smoke ($products.Count -eq 1 -and $products[0].Context -eq 2 -and $products[0].UserSid -eq $userSid) "Upgrade leaves one per-user installed product"
    Assert-Smoke ([IO.File]::ReadAllText($marker) -eq $markerValue) "Upgrade preserved the synthetic user-data marker"
    Run-Msi "/i" (Join-Path $testRoot "initial.msi") "03-downgrade" $true
    Assert-Smoke ((& (Join-Path $install "cobot.exe") version) -eq $scope.upgradeVersion) "Refused downgrade preserved the newer application"
    Run-Msi "/i" (Join-Path $testRoot "duplicate.msi") "04-equal-version" $true
    Assert-Smoke (@(Installed-CoBot).Count -eq 1) "Equal-version package did not create a duplicate product"
    Run-Msi "/x" (Join-Path $testRoot "upgrade.msi") "05-uninstall"
    Assert-Smoke (@(Installed-CoBot).Count -eq 0) "Uninstall removed product registration"
    Assert-Smoke (-not (Test-Path -LiteralPath (Join-Path $install "cobot-desktop.exe"))) "Uninstall removed installed program files"
    Assert-Smoke (-not (Test-Path -LiteralPath $install)) "Uninstall removed the empty program folder"
    Assert-Smoke (-not (Test-Path -LiteralPath $menu)) "Uninstall removed the application shortcut"
    Assert-Smoke ([IO.File]::ReadAllText($marker) -eq $markerValue) "Uninstall preserved the synthetic user-data marker"
    Run-Msi "/i" (Join-Path $testRoot "upgrade.msi") "06-reinstall"
    Assert-Smoke ((& (Join-Path $install "cobot.exe") version) -eq $scope.upgradeVersion) "Reinstallation works"
    $result.passed = $true
    $result.manualChecks = @("MSI wizard appearance", "Files-in-use prompt with running work", "WebView2 download/install when missing")
    # The visible desktop uses an explicit synthetic home in the guest. It
    # never opens the host user's real profile or imports their Codex login.
    $guestHome = Join-Path $testRoot "desktop-home"
    New-Item -ItemType Directory -Path $guestHome -Force | Out-Null
    $start = New-Object Diagnostics.ProcessStartInfo
    $start.FileName = Join-Path $install "cobot-desktop.exe"
    $start.WorkingDirectory = $install
    $start.UseShellExecute = $false
    $start.EnvironmentVariables["USERPROFILE"] = $guestHome
    [void][Diagnostics.Process]::Start($start)
} catch {
    $result.passed = $false
    $result.error = $_.Exception.Message
} finally {
    if (Test-Path -LiteralPath $output) {
        $result | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $output "result.json") -Encoding utf8
    }
}
if (-not $result.passed) { throw $result.error }
