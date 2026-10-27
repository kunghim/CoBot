# 实测用的接口辅助。每次 shell 调用都是新进程，所以每次点源都会重新取会话。
#   . scripts/live-test-api.ps1 -Port 18861
#   Api '/api/v1/engine'                          # GET，返回响应里的 data
#   Api '/api/v1/settings' 'PATCH' @{ language = 'en' }
#   Stop-CobotCore -SecretFile "$R/real/runtime/credentials/instance-admin.secret"
# 只连本机端口；写请求自动带 CSRF 和 Origin。
param([int]$Port = 18861)

$script:CobotBase = "http://127.0.0.1:$Port"
$script:CobotSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
# 只有 Accept: text/html 的文档请求才会下发会话 cookie
$null = Invoke-WebRequest "$script:CobotBase/" -WebSession $script:CobotSession -Headers @{ Accept = 'text/html' } -UseBasicParsing

function Api([string]$Path, [string]$Method = 'GET', $Body = $null, [string]$IfMatch = '') {
  $headers = @{ Accept = 'application/json' }
  if ($IfMatch -ne '') { $headers['If-Match'] = $IfMatch }
  if ($Method -ne 'GET') {
    $csrf = ($script:CobotSession.Cookies.GetCookies($script:CobotBase) | Where-Object Name -like 'cobot_csrf_*').Value
    $headers['X-CSRF-Token'] = $csrf
    $headers['Origin'] = $script:CobotBase
    # 写接口要求幂等键：请求体里要有 requestId（≤128 字符），没有就补一个
    if ($null -ne $Body -and -not ($Body.PSObject.Properties['requestId'])) {
      $Body | Add-Member -NotePropertyName requestId -NotePropertyValue ([guid]::NewGuid().ToString())
    }
    # DELETE 要求同名请求头；与请求体里的 requestId 保持一致
    if ($null -ne $Body -and $Body.PSObject.Properties['requestId']) {
      $headers['Idempotency-Key'] = $Body.requestId
    }
  }
  $call = @{ Uri = "$script:CobotBase$Path"; Method = $Method; WebSession = $script:CobotSession; Headers = $headers }
  if ($null -ne $Body) {
    $call.Body = ($Body | ConvertTo-Json -Depth 8 -Compress)
    $call.ContentType = 'application/json'
  }
  (Invoke-RestMethod @call).data
}

# 优雅停掉本脚本连着的核心；不要杀进程，也别碰其他会话启动的核心。
function Stop-CobotCore([string]$SecretFile) {
  $secret = (Get-Content $SecretFile -Raw).Trim()
  $stop = @{
    Uri         = "$script:CobotBase/internal/v1/service/stop"
    Method      = 'POST'
    Headers     = @{ 'X-Instance-Secret' = $secret }
    ContentType = 'application/json'
    Body        = (@{ requestId = [guid]::NewGuid().ToString() } | ConvertTo-Json -Compress)
  }
  (Invoke-WebRequest @stop -UseBasicParsing).StatusCode
}
