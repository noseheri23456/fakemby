# FakEmby 快速启动脚本（一键启动 + 导入测试数据）

Write-Host "╔════════════════════════════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║              FakEmby 服务器 - 快速启动                         ║" -ForegroundColor Cyan
Write-Host "╚════════════════════════════════════════════════════════════════╝" -ForegroundColor Cyan
Write-Host ""

$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $scriptPath

Write-Host "1️⃣  清理旧进程..." -ForegroundColor Yellow
Get-Process fakemby -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 1

Write-Host "2️⃣  编译服务器..." -ForegroundColor Yellow
$env:CGO_ENABLED = 0
go build -o fakemby.exe ./cmd/fakemby
if (!$?) {
    Write-Host "✗ 编译失败" -ForegroundColor Red
    exit 1
}
Write-Host "✓ 编译成功" -ForegroundColor Green

Write-Host "3️⃣  启动服务器..." -ForegroundColor Yellow
Start-Process -WindowStyle Minimized -FilePath .\fakemby.exe
Start-Sleep -Seconds 3
Write-Host "✓ 服务器已启动 (http://localhost:8096)" -ForegroundColor Green

Write-Host ""
Write-Host "4️⃣  导入测试数据..." -ForegroundColor Yellow

# 导入电影
$json = @{
    library = "Movies"
    items = @(
        @{
            name = "Big Buck Bunny"
            type = "Movie"
            year = 2008
            overview = "A big buck is leading a small, defenseless rabbit astray into the woods."
            sources = @(
                @{
                    name = "1080p"
                    url = "https://peach.blender.org/download/960/?token=eyJfZXhwaXJlIjogMTcwMzE2NjMxOH0%3D.oa4rHNp46WnWdtm0A6Uk9SY2c3w%3D"
                    container = "mkv"
                }
            )
        }
    )
} | ConvertTo-Json -Depth 10

$response = Invoke-WebRequest -Uri "http://localhost:8096/api/admin/import" `
    -Method POST `
    -Headers @{"Content-Type"="application/json"; "X-Api-Key"="change-me"} `
    -Body $json -ErrorAction Stop
$result = $response.Content | ConvertFrom-Json
Write-Host "  ✓ 导入电影库：$($result.imported) 项" -ForegroundColor Green

# 导入电视剧
$json = @{
    library = "TV Shows"
    items = @(
        @{
            name = "Test Series"
            type = "Series"
            year = 2024
            overview = "A test series for demonstration"
            seasons = @(
                @{
                    season_number = 1
                    episodes = @(
                        @{
                            name = "Episode 1"
                            episode_number = 1
                            overview = "First episode of test series"
                            sources = @(
                                @{
                                    name = "480p"
                                    url = "https://example.com/s01e01.mp4"
                                    container = "mp4"
                                }
                            )
                        },
                        @{
                            name = "Episode 2"
                            episode_number = 2
                            overview = "Second episode of test series"
                            sources = @(
                                @{
                                    name = "480p"
                                    url = "https://example.com/s01e02.mp4"
                                    container = "mp4"
                                }
                            )
                        }
                    )
                }
            )
        }
    )
} | ConvertTo-Json -Depth 10

$response = Invoke-WebRequest -Uri "http://localhost:8096/api/admin/import" `
    -Method POST `
    -Headers @{"Content-Type"="application/json"; "X-Api-Key"="change-me"} `
    -Body $json -ErrorAction Stop
$result = $response.Content | ConvertFrom-Json
Write-Host "  ✓ 导入电视剧库：$($result.imported) 项" -ForegroundColor Green

Write-Host ""
Write-Host "════════════════════════════════════════════════════════════════" -ForegroundColor Green
Write-Host "✅ 启动完成！" -ForegroundColor Green
Write-Host "════════════════════════════════════════════════════════════════" -ForegroundColor Green
Write-Host ""
Write-Host "📱 在 RodelPlayer 中连接：" -ForegroundColor Cyan
Write-Host "   地址：http://localhost:8096" -ForegroundColor Yellow
Write-Host "   用户名：admin" -ForegroundColor Yellow
Write-Host "   密码：admin" -ForegroundColor Yellow
Write-Host ""
Write-Host "📊 API 端点：" -ForegroundColor Cyan
Write-Host "   系统信息：http://localhost:8096/emby/System/Info/Public" -ForegroundColor Yellow
Write-Host "   用户列表：http://localhost:8096/emby/Users/Public" -ForegroundColor Yellow
Write-Host ""
Write-Host "💡 提示：" -ForegroundColor Cyan
Write-Host "   • 第一次连接需要登录，输入 admin/admin" -ForegroundColor Yellow
Write-Host "   • 服务器窗口最小化运行在后台" -ForegroundColor Yellow
Write-Host "   • 要停止服务器，运行：Stop-Process -Name fakemby" -ForegroundColor Yellow
Write-Host ""
