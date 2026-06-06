# FakEmby 完整认证流程测试脚本
# 模拟官方 Emby 客户端的连接流程

Write-Host "╔════════════════════════════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║           FakEmby 认证流程测试                                 ║" -ForegroundColor Cyan
Write-Host "╚════════════════════════════════════════════════════════════════╝" -ForegroundColor Cyan
Write-Host ""

$serverUrl = "http://localhost:8096"

# 步骤 1: 获取公开系统信息（无需认证）
Write-Host "步骤 1️⃣: 获取公开系统信息（无需认证）" -ForegroundColor Yellow
try {
    $response = Invoke-WebRequest -Uri "$serverUrl/emby/System/Info/Public" -ErrorAction Stop
    Write-Host "✓ 成功 (200)" -ForegroundColor Green
    $data = $response.Content | ConvertFrom-Json
    Write-Host "  服务器: $($data.ServerName)"
    Write-Host "  版本: $($data.Version)"
    Write-Host "  ID: $($data.Id)"
} catch {
    Write-Host "✗ 失败: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
    exit 1
}

Write-Host ""

# 步骤 2: 获取可用用户列表（无需认证）
Write-Host "步骤 2️⃣: 获取可用用户列表（无需认证）" -ForegroundColor Yellow
try {
    $response = Invoke-WebRequest -Uri "$serverUrl/emby/Users/Public" -ErrorAction Stop
    Write-Host "✓ 成功 (200)" -ForegroundColor Green
    $data = $response.Content | ConvertFrom-Json
    Write-Host "  可用用户数: $($data.Users.Count)"
    foreach ($user in $data.Users) {
        Write-Host "    - $($user.Name) (ID: $($user.Id))"
    }
} catch {
    Write-Host "✗ 失败: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
    exit 1
}

Write-Host ""

# 步骤 3: 登录获取 Token
Write-Host "步骤 3️⃣: 登录获取 Token" -ForegroundColor Yellow
try {
    $response = Invoke-WebRequest -Uri "$serverUrl/emby/Users/AuthenticateByName" `
        -Method POST `
        -Headers @{"Content-Type"="application/json"} `
        -Body '{"Username":"admin","Pw":"admin"}' `
        -ErrorAction Stop

    Write-Host "✓ 登录成功 (200)" -ForegroundColor Green
    $data = $response.Content | ConvertFrom-Json
    $token = $data.AccessToken
    $userId = $data.User.Id

    Write-Host "  Token: $token"
    Write-Host "  User ID: $userId"
    Write-Host "  用户名: $($data.User.Name)"
    Write-Host "  是否管理员: $($data.User.IsAdmin)"
} catch {
    Write-Host "✗ 登录失败: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
    exit 1
}

Write-Host ""

# 步骤 4: 用 Token 访问受保护端点（多种方式测试）
Write-Host "步骤 4️⃣: 用 Token 访问受保护端点" -ForegroundColor Yellow

# 方式 A: X-Emby-Token Header
Write-Host ""
Write-Host "  方式 A: X-Emby-Token Header" -ForegroundColor Cyan
try {
    $response = Invoke-WebRequest -Uri "$serverUrl/emby/System/Info" `
        -Headers @{"X-Emby-Token"=$token} `
        -ErrorAction Stop
    Write-Host "  ✓ 成功 (200)" -ForegroundColor Green
} catch {
    Write-Host "  ✗ 失败: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
}

# 方式 B: Bearer Token
Write-Host ""
Write-Host "  方式 B: Authorization: Bearer {token}" -ForegroundColor Cyan
try {
    $response = Invoke-WebRequest -Uri "$serverUrl/emby/System/Info" `
        -Headers @{"Authorization"="Bearer $token"} `
        -ErrorAction Stop
    Write-Host "  ✓ 成功 (200)" -ForegroundColor Green
} catch {
    Write-Host "  ✗ 失败: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
}

# 方式 C: 查询参数
Write-Host ""
Write-Host "  方式 C: 查询参数 ?api_key=" -ForegroundColor Cyan
try {
    $response = Invoke-WebRequest -Uri "$serverUrl/emby/System/Info?api_key=$token" `
        -ErrorAction Stop
    Write-Host "  ✓ 成功 (200)" -ForegroundColor Green
} catch {
    Write-Host "  ✗ 失败: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
}

Write-Host ""

# 步骤 5: 获取用户媒体库
Write-Host "步骤 5️⃣: 获取用户媒体库" -ForegroundColor Yellow
try {
    $response = Invoke-WebRequest -Uri "$serverUrl/emby/Users/$userId/Views" `
        -Headers @{"X-Emby-Token"=$token} `
        -ErrorAction Stop

    Write-Host "✓ 成功 (200)" -ForegroundColor Green
    $data = $response.Content | ConvertFrom-Json
    Write-Host "  媒体库数: $($data.Items.Count)"
    foreach ($item in $data.Items) {
        Write-Host "    - $($item.Name) (Type: $($item.Type))"
    }
} catch {
    Write-Host "✗ 失败: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
}

Write-Host ""

# 步骤 6: 获取用户媒体项目
Write-Host "步骤 6️⃣: 获取用户媒体项目" -ForegroundColor Yellow
try {
    $response = Invoke-WebRequest -Uri "$serverUrl/emby/Users/$userId/Items" `
        -Headers @{"X-Emby-Token"=$token} `
        -ErrorAction Stop

    Write-Host "✓ 成功 (200)" -ForegroundColor Green
    $data = $response.Content | ConvertFrom-Json
    Write-Host "  总项目数: $($data.TotalRecordCount)"
    if ($data.Items.Count -gt 0) {
        foreach ($item in $data.Items | Select-Object -First 3) {
            Write-Host "    - $($item.Name) (Type: $($item.Type))"
        }
        if ($data.Items.Count -gt 3) {
            Write-Host "    ... 以及其他 $($data.Items.Count - 3) 项"
        }
    }
} catch {
    Write-Host "✗ 失败: $($_.Exception.Response.StatusCode)" -ForegroundColor Red
}

Write-Host ""
Write-Host "════════════════════════════════════════════════════════════════" -ForegroundColor Green
Write-Host "✅ 认证流程测试完成！" -ForegroundColor Green
Write-Host "════════════════════════════════════════════════════════════════" -ForegroundColor Green
Write-Host ""
Write-Host "如果所有步骤都成功，说明服务器认证系统正常。" -ForegroundColor Cyan
Write-Host "如果客户端仍然无法连接，可能是客户端缓存问题。" -ForegroundColor Cyan
Write-Host ""
Write-Host "建议："
Write-Host "1. 完全卸载并重新安装客户端（清除应用数据）"
Write-Host "2. 重新添加服务器 http://localhost:8096"
Write-Host "3. 在登录提示中输入 admin / admin"
