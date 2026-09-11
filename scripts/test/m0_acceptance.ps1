#Requires -Version 5.1
<#
  FakEmby M0 acceptance script (ROADMAP appendix A).

  Starts a throwaway server on a temp DB, then asserts:
    S1  /api/admin/* requires a real X-Api-Key ("change-me" no longer works)
    S7  admin api_key is no longer a universal /emby/ token
    S5  cross-user reads are rejected with 403
    S6  no plaintext password in logs
    S3  PlaybackInfo never echoes the caller's token
    M0-7 signed DirectStreamUrl + /api/auth/verify
    M0-8 FAKEMBY_SERVER_PORT env override

  Usage: .\scripts\test\m0_acceptance.ps1 [-Port 18096]
#>
param(
    [int]$Port = 18096,
    [string]$AdminKey = "m0-test-admin-key"
)

$ErrorActionPreference = "Continue"
$base = "http://localhost:$Port"
$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$tmp = Join-Path $root ".workbuddy\m0-acceptance"
$dbPath = Join-Path $tmp "m0.db"
$logPath = Join-Path $tmp "fakemby.log"

$script:failures = 0
$script:passes = 0

function Assert($name, $expected, $actual) {
    if ("$expected" -eq "$actual") {
        $script:passes++
        Write-Host "  PASS  $name ($actual)" -ForegroundColor Green
    } else {
        $script:failures++
        Write-Host "  FAIL  $name : expected $expected, got $actual" -ForegroundColor Red
    }
}

# NB: PowerShell 5.1 mangles quotes when passing JSON inline to native commands,
# so bodies are written to a temp file and passed via curl's @file syntax.
$bodyFile = Join-Path $tmp "body.json"

function HttpCode([string]$method, [string]$url, [hashtable]$headers, [string]$body) {
    $args = @("-s", "-o", "NUL", "-w", "%{http_code}", "-X", $method, $url)
    foreach ($k in $headers.Keys) { $args += @("-H", "$k`: $($headers[$k])") }
    if ($body) {
        Set-Content -Path $bodyFile -Value $body -Encoding ASCII
        $args += @("-H", "Content-Type: application/json", "--data-binary", "@$bodyFile")
    }
    return (curl.exe @args).Trim()
}

function HttpBody([string]$method, [string]$url, [hashtable]$headers, [string]$body) {
    $args = @("-s", "-X", $method, $url)
    foreach ($k in $headers.Keys) { $args += @("-H", "$k`: $($headers[$k])") }
    if ($body) {
        Set-Content -Path $bodyFile -Value $body -Encoding ASCII
        $args += @("-H", "Content-Type: application/json", "--data-binary", "@$bodyFile")
    }
    return (curl.exe @args | Out-String)
}

# ---------------------------------------------------------------- setup
Write-Host "== M0 acceptance ==" -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
Remove-Item "$dbPath*" -Force -ErrorAction SilentlyContinue
Remove-Item $logPath -Force -ErrorAction SilentlyContinue

Write-Host "[1/6] building..."
Set-Location $root
$env:CGO_ENABLED = "0"
go build -o "$tmp\fakemby.exe" ./cmd/fakemby
if ($LASTEXITCODE -ne 0) { Write-Host "build failed" -ForegroundColor Red; exit 1 }

Write-Host "[2/6] starting server on $Port ..."
$pinfo = New-Object System.Diagnostics.ProcessStartInfo
$pinfo.FileName = "$tmp\fakemby.exe"
$pinfo.WorkingDirectory = $tmp
$pinfo.UseShellExecute = $false
$pinfo.EnvironmentVariables["FAKEMBY_SERVER_PORT"] = "$Port"
$pinfo.EnvironmentVariables["FAKEMBY_DATABASE_PATH"] = $dbPath
$pinfo.EnvironmentVariables["FAKEMBY_ADMIN_API_KEY"] = $AdminKey
$pinfo.EnvironmentVariables["FAKEMBY_LOG_FILE"] = $logPath
$pinfo.EnvironmentVariables["FAKEMBY_LOG_LEVEL"] = "info"
$pinfo.EnvironmentVariables["FAKEMBY_PLAYBACK_SIGN_TTL"] = "600"
$proc = [System.Diagnostics.Process]::Start($pinfo)

try {
    $ready = $false
    for ($i = 0; $i -lt 40; $i++) {
        Start-Sleep -Milliseconds 250
        $c = HttpCode "GET" "$base/emby/System/Info/Public" @{}
        if ($c -eq "200") { $ready = $true; break }
    }
    if (-not $ready) { Write-Host "server did not become ready (port $Port)" -ForegroundColor Red; exit 1 }

    $adminHeaders = @{ "X-Api-Key" = $AdminKey }

    # ------------------------------------------------------------ S1
    Write-Host "[3/6] S1 admin write endpoints require a real key"
    foreach ($r in @(
        @{ m = "POST"; u = "/api/admin/items" },
        @{ m = "PUT"; u = "/api/admin/items/nope" },
        @{ m = "DELETE"; u = "/api/admin/items/nope" },
        @{ m = "POST"; u = "/api/admin/items/nope/sources" },
        @{ m = "DELETE"; u = "/api/admin/items/nope/sources/nope" }
    )) { Assert "$($r.m) $($r.u) without key" 401 (HttpCode $r.m "$base$($r.u)" @{} "{}") }

    Assert "GET /api/admin/stats with change-me" 401 (HttpCode "GET" "$base/api/admin/stats" @{ "X-Api-Key" = "change-me" })
    Assert "GET /api/admin/stats with real key" 200 (HttpCode "GET" "$base/api/admin/stats" $adminHeaders)

    # ------------------------------------------------------------ S7
    Write-Host "[4/6] S7 admin api_key is not an /emby/ token"
    Assert "GET /emby/Users with admin api_key" 401 (HttpCode "GET" "$base/emby/Users" @{ "X-Emby-Token" = $AdminKey })

    # ------------------------------------------------------------ seed
    $login = HttpBody "POST" "$base/emby/Users/AuthenticateByName" @{} '{"Username":"admin","Pw":"admin"}' | ConvertFrom-Json
    $adminToken = $login.AccessToken
    if (-not $adminToken) { Write-Host "admin login failed" -ForegroundColor Red; exit 1 }
    $adminId = $login.User.Id

    HttpBody "POST" "$base/api/admin/users" $adminHeaders '{"name":"bob","password":"bobpass123","is_admin":false}' | Out-Null
    $bobLogin = HttpBody "POST" "$base/emby/Users/AuthenticateByName" @{} '{"Username":"bob","Pw":"bobpass123"}' | ConvertFrom-Json
    $bobToken = $bobLogin.AccessToken

    # import one movie so playback endpoints have something to serve
    $importBody = '{"library":"Movies","items":[{"name":"M0 Test Movie","type":"Movie","year":2026,"sources":[{"name":"HD","url":"https://example.com/m0.mp4","container":"mp4"}],"subtitles":[{"language":"chi","title":"Chinese","url":"https://example.com/m0.srt","codec":"srt"}]}]}'
    HttpBody "POST" "$base/api/admin/import" $adminHeaders $importBody | Out-Null
    $items = HttpBody "GET" "$base/emby/Users/$adminId/Items?Recursive=true&IncludeItemTypes=Movie" @{ "X-Emby-Token" = $adminToken } | ConvertFrom-Json
    $itemId = $items.Items[0].Id

    # ------------------------------------------------------------ S5
    Write-Host "[5/6] S5 horizontal privilege escalation"
    Assert "bob reads admin's items" 403 (HttpCode "GET" "$base/emby/Users/$adminId/Items?Filters=IsFavorite" @{ "X-Emby-Token" = $bobToken })
    Assert "bob reads own items" 200 (HttpCode "GET" "$base/emby/Users/$($bobLogin.User.Id)/Items" @{ "X-Emby-Token" = $bobToken })
    Assert "admin reads bob's items" 200 (HttpCode "GET" "$base/emby/Users/$($bobLogin.User.Id)/Items" @{ "X-Emby-Token" = $adminToken })

    # ------------------------------------------------------------ S3 / M0-7
    Write-Host "[6/6] S3 token leak + M0-7 signing"
    $pb = HttpBody "POST" "$base/emby/Items/$itemId/PlaybackInfo" @{ "X-Emby-Token" = $adminToken } '{"MediaSourceId":null}' | ConvertFrom-Json
    $direct = $pb.MediaSources[0].DirectStreamUrl
    if ([string]::IsNullOrEmpty($direct)) { Write-Host "  FAIL  DirectStreamUrl missing" -ForegroundColor Red; $script:failures++ }
    if ($direct -like "*$adminToken*") { Write-Host "  FAIL  token leaked into DirectStreamUrl" -ForegroundColor Red; $script:failures++ }
    else { Write-Host "  PASS  DirectStreamUrl carries no token" -ForegroundColor Green; $script:passes++ }
    if ($direct -like "*exp=*" -and $direct -like "*sig=*") { Write-Host "  PASS  DirectStreamUrl is signed" -ForegroundColor Green; $script:passes++ }
    else { Write-Host "  FAIL  DirectStreamUrl not signed: $direct" -ForegroundColor Red; $script:failures++ }

    # /api/auth/verify round-trip
    $query = $direct.Substring($direct.IndexOf("?") + 1)
    $qs = @{}
    foreach ($pair in $query.Split("&")) { $kv = $pair.Split("=", 2); $qs[$kv[0]] = $kv[1] }
    $srcId = $pb.MediaSources[0].Id
    $verifyUrl = "$base/api/auth/verify?type=video&item_id=$itemId&source_id=$srcId&uid=$adminId&exp=$($qs['exp'])&sig=$($qs['sig'])"
    Assert "verify valid signature" (HttpCode "GET" $verifyUrl @{}).Trim() 200
    Assert "verify tampered signature" 401 (HttpCode "GET" "$($verifyUrl)tampered" @{})
    Assert "verify expired signature" 401 (HttpCode "GET" "$base/api/auth/verify?type=video&item_id=$itemId&source_id=$srcId&uid=$adminId&exp=1&sig=$($qs['sig'])" @{})

    # signed URL actually authorizes the stream (no token at all)
    Assert "stream with signature only" 302 (HttpCode "GET" "$base/emby/Videos/$itemId/stream?Static=true&mediaSourceId=$srcId&uid=$adminId&exp=$($qs['exp'])&sig=$($qs['sig'])" @{})
    Assert "stream without any auth" 401 (HttpCode "GET" "$base/emby/Videos/$itemId/stream?Static=true&mediaSourceId=$srcId" @{})

    # ------------------------------------------------------------ S6
    Start-Sleep -Milliseconds 400
    if (Test-Path $logPath) {
        $leak = Select-String -Path $logPath -SimpleMatch -Pattern "bobpass123" -ErrorAction SilentlyContinue
        if ($leak) { Write-Host "  FAIL  plaintext password found in log" -ForegroundColor Red; $script:failures++ }
        else { Write-Host "  PASS  no plaintext password in log" -ForegroundColor Green; $script:passes++ }
    } else {
        Write-Host "  SKIP  log file not created at $logPath" -ForegroundColor Yellow
    }

    # ------------------------------------------------------------ M0-6
    Write-Host "    M0-6 CORS whitelist + public user list"
    $defaultHeaders = (curl.exe -s -D - -o NUL -H "Origin: https://evil.example" "$base/emby/Users/Public" | Out-String)
    if ($defaultHeaders -match "Access-Control-Allow-Origin") {
        Write-Host "  FAIL  default config leaks CORS header" -ForegroundColor Red; $script:failures++
    } else { Write-Host "  PASS  default config is same-origin (no ACAO)" -ForegroundColor Green; $script:passes++ }

    $public = HttpBody "GET" "$base/emby/Users/Public" @{}
    if ($public -match "IsAdmin" -or $public -match "Policy") {
        Write-Host "  FAIL  /emby/Users/Public still exposes IsAdmin/Policy" -ForegroundColor Red; $script:failures++
    } else { Write-Host "  PASS  /emby/Users/Public hides IsAdmin/Policy" -ForegroundColor Green; $script:passes++ }

    # ------------------------------------------------------------ M0-8
    Write-Host "  INFO  server listened on FAKEMBY_SERVER_PORT=$Port (env override works)" -ForegroundColor Gray
}
finally {
    if ($proc -and -not $proc.HasExited) { $proc.Kill(); $proc.WaitForExit(3000) | Out-Null }
}

# ---------------------------------------------------------------- CORS phase
# 第二个进程：分别用白名单与 "*" 启动，验证 M0-6 的两种输出形态
Write-Host "[+] CORS phase"
foreach ($scenario in @(
    @{ origins = "https://good.example"; expectOrigin = "https://good.example"; expectCreds = $true },
    @{ origins = "*"; expectOrigin = "*"; expectCreds = $false }
)) {
    $p2info = New-Object System.Diagnostics.ProcessStartInfo
    $p2info.FileName = "$tmp\fakemby.exe"
    $p2info.WorkingDirectory = $tmp
    $p2info.UseShellExecute = $false
    $p2info.EnvironmentVariables["FAKEMBY_SERVER_PORT"] = "$($Port + 1)"
    $p2info.EnvironmentVariables["FAKEMBY_DATABASE_PATH"] = $dbPath
    $p2info.EnvironmentVariables["FAKEMBY_ADMIN_API_KEY"] = $AdminKey
    $p2info.EnvironmentVariables["FAKEMBY_SERVER_CORS_ORIGINS"] = $scenario.origins
    $p2info.EnvironmentVariables["FAKEMBY_LOG_LEVEL"] = "error"
    $p2 = [System.Diagnostics.Process]::Start($p2info)
    try {
        $b2 = "http://localhost:$($Port + 1)"
        for ($i = 0; $i -lt 40; $i++) {
            Start-Sleep -Milliseconds 200
            if ((HttpCode "GET" "$b2/emby/System/Info/Public" @{}) -eq "200") { break }
        }
        $h = (curl.exe -s -D - -o NUL -H "Origin: https://good.example" "$b2/emby/Users/Public" | Out-String)
        if ($h -match "Access-Control-Allow-Origin: (\S+)") {
            Assert "cors '$($scenario.origins)' -> ACAO" $scenario.expectOrigin $Matches[1].Trim()
        } else { Write-Host "  FAIL  no ACAO header for '$($scenario.origins)'" -ForegroundColor Red; $script:failures++ }

        $hasCreds = $h -match "Access-Control-Allow-Credentials"
        Assert "cors '$($scenario.origins)' -> credentials" $scenario.expectCreds $hasCreds

        # 白名单模式下，非白名单来源不应拿到 ACAO
        if ($scenario.origins -ne "*") {
            $h2 = (curl.exe -s -D - -o NUL -H "Origin: https://evil.example" "$b2/emby/Users/Public" | Out-String)
            if ($h2 -match "Access-Control-Allow-Origin") {
                Write-Host "  FAIL  non-whitelisted origin got ACAO" -ForegroundColor Red; $script:failures++
            } else { Write-Host "  PASS  non-whitelisted origin gets no ACAO" -ForegroundColor Green; $script:passes++ }
        }
    }
    finally {
        if ($p2 -and -not $p2.HasExited) { $p2.Kill(); $p2.WaitForExit(3000) | Out-Null }
    }
}

Write-Host ""
Write-Host "passed=$script:passes failed=$script:failures" -ForegroundColor Cyan
if ($script:failures -gt 0) { exit 1 }
exit 0
