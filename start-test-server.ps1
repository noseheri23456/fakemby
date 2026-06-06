# FakEmby 测试服务器启动脚本 (PowerShell)
# 功能: 编译、启动服务器、导入测试数据

$ErrorActionPreference = "Continue"
$ProgressPreference = "SilentlyContinue"

# 设置项目目录
$ProjectDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ProjectDir

Write-Host ""
Write-Host "╔════════════════════════════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║                  FakEmby 测试服务器启动器                       ║" -ForegroundColor Cyan
Write-Host "╚════════════════════════════════════════════════════════════════╝" -ForegroundColor Cyan
Write-Host ""

# [1] 清理旧进程
Write-Host "[1/6] 清理旧进程..." -ForegroundColor Yellow
$existingProcess = Get-Process | Where-Object { $_.Name -eq "fakemby" -or ($_.Name -eq "go" -and $_.CommandLine -like "*fakemby*") }
if ($existingProcess) {
    $existingProcess | Stop-Process -Force -ErrorAction SilentlyContinue
    Write-Host "  ✓ 旧进程已清理" -ForegroundColor Green
    Start-Sleep -Seconds 2
}

# [2] 清理构建缓存
Write-Host ""
Write-Host "[2/6] 清理构建缓存..." -ForegroundColor Yellow
if (Test-Path "go.mod") {
    go clean -cache 2>$null
    Write-Host "  ✓ 缓存已清理" -ForegroundColor Green
}

# [3] 编译项目
Write-Host ""
Write-Host "[3/6] 编译项目..." -ForegroundColor Yellow
$env:CGO_ENABLED = 0
$buildOutput = go build -a -o fakemby.exe ./cmd/fakemby 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host "  ✗ 编译失败！" -ForegroundColor Red
    Write-Host $buildOutput
    Read-Host "按 Enter 继续"
    exit 1
}
Write-Host "  ✓ 编译成功" -ForegroundColor Green

# [4] 启动服务器
Write-Host ""
Write-Host "[4/6] 启动服务器..." -ForegroundColor Yellow
Start-Process -FilePath ".\fakemby.exe" -WindowStyle Normal -PassThru | Out-Null
Start-Sleep -Seconds 3

# 检查服务器是否启动
$maxRetries = 10
$retryCount = 0
$serverReady = $false

do {
    try {
        $response = Invoke-WebRequest -Uri "http://localhost:8096/emby/System/Info/Public" -ErrorAction SilentlyContinue
        if ($response.StatusCode -eq 200) {
            $serverReady = $true
            Write-Host "  ✓ 服务器已启动" -ForegroundColor Green
            break
        }
    }
    catch {
        $retryCount++
        if ($retryCount -lt $maxRetries) {
            Start-Sleep -Seconds 1
        }
    }
} while ($retryCount -lt $maxRetries)

if (-not $serverReady) {
    Write-Host "  ✗ 服务器启动失败" -ForegroundColor Red
    Write-Host "  请检查是否已安装 Go，或查看之前的输出"
    Read-Host "按 Enter 继续"
    exit 1
}

# [5] 导入测试数据
Write-Host ""
Write-Host "[5/6] 导入测试数据..." -ForegroundColor Yellow

if (Test-Path "import_test_data.py") {
    try {
        $output = python import_test_data.py 2>&1
        if ($LASTEXITCODE -eq 0) {
            Write-Host "  ✓ 数据导入成功" -ForegroundColor Green
        } else {
            Write-Host "  ⚠ 数据导入可能失败，检查输出：" -ForegroundColor Yellow
            Write-Host $output
        }
    }
    catch {
        Write-Host "  ⚠ 数据导入脚本执行失败" -ForegroundColor Yellow
    }
} elseif (Test-Path "import_test_data.sh") {
    try {
        bash import_test_data.sh 2>&1 | Out-Null
        if ($LASTEXITCODE -eq 0) {
            Write-Host "  ✓ 数据导入成功" -ForegroundColor Green
        } else {
            Write-Host "  ⚠ 数据导入失败，可以手动运行: bash import_test_data.sh" -ForegroundColor Yellow
        }
    }
    catch {
        Write-Host "  ⚠ 需要安装 Git Bash 或 WSL 来运行导入脚本" -ForegroundColor Yellow
    }
} else {
    Write-Host "  ⚠ 导入脚本不存在，跳过自动导入" -ForegroundColor Yellow
    Write-Host "  可以手动创建导入数据或在浏览器中访问配置" -ForegroundColor Yellow
}

# [6] 显示信息
Write-Host ""
Write-Host "[6/6] 配置信息" -ForegroundColor Yellow
Write-Host "════════════════════════════════════════════════════════════════"
Write-Host ""
Write-Host "✓ 服务器已启动！" -ForegroundColor Green
Write-Host ""
Write-Host "📊 服务器信息:" -ForegroundColor Cyan
Write-Host "  地址:       http://localhost:8096"
Write-Host "  默认用户:   admin"
Write-Host "  默认密码:   admin"
Write-Host ""
Write-Host "🎬 测试数据:" -ForegroundColor Cyan
Write-Host "  - 电影库: Big Buck Bunny (1080p + 480p)"
Write-Host "  - 电视剧库: Test Series (Season 1, 2 episodes)"
Write-Host ""
Write-Host "🔗 快速链接:" -ForegroundColor Cyan
Write-Host "  系统信息:   http://localhost:8096/emby/System/Info/Public"
Write-Host "  浏览器打开: http://localhost:8096"
Write-Host ""
Write-Host "💡 客户端连接:" -ForegroundColor Cyan
Write-Host "  小幻影视 / SenPlayer:"
Write-Host "  1. 打开应用"
Write-Host "  2. 添加服务器: http://localhost:8096"
Write-Host "  3. 用户: admin"
Write-Host "  4. 密码: admin"
Write-Host ""
Write-Host "🧪 API 测试 (PowerShell):" -ForegroundColor Cyan
Write-Host "  获取 Token:"
Write-Host '  `Invoke-WebRequest -Uri "http://localhost:8096/emby/Users/AuthenticateByName" `'
Write-Host '    -Method POST `'
Write-Host '    -Headers @{"Content-Type"="application/json"} `'
Write-Host '    -Body "{`"Username`":`"admin`",`"Pw`":`"admin`"}" | ConvertFrom-Json'
Write-Host ""
Write-Host "📖 更多信息:" -ForegroundColor Cyan
Write-Host "  查看 README.md 了解完整 API 文档"
Write-Host "  查看 TESTING.md 了解测试指南"
Write-Host ""
Write-Host "════════════════════════════════════════════════════════════════"
Write-Host ""
Write-Host "✨ 按任意键打开浏览器进行测试..." -ForegroundColor Green
Read-Host

# 打开浏览器
Start-Process "http://localhost:8096"
