@echo off
REM FakEmby 测试服务器启动脚本
REM 功能: 编译、启动服务器、导入测试数据

setlocal enabledelayedexpansion

echo.
echo ╔════════════════════════════════════════════════════════════════╗
echo ║                  FakEmby 测试服务器启动器                       ║
echo ╚════════════════════════════════════════════════════════════════╝
echo.

REM 获取脚本所在目录
cd /d "%~dp0"
set PROJECT_DIR=%cd%

echo [1/6] 清理旧进程...
for /f "tokens=5" %%a in ('netstat -ano ^| findstr ":8096"') do (
    echo   杀死进程 %%a
    taskkill /PID %%a /F 2>nul
)
timeout /t 2 /nobreak >nul

echo.
echo [2/6] 清理构建缓存...
if exist "go.mod" (
    go clean -cache >nul 2>&1
    echo   ✓ 缓存已清理
)

echo.
echo [3/6] 编译项目...
set CGO_ENABLED=0
go build -a -o fakemby.exe ./cmd/fakemby
if !errorlevel! neq 0 (
    echo   ✗ 编译失败！
    echo   请检查 Go 环境和项目配置
    pause
    exit /b 1
)
echo   ✓ 编译成功

echo.
echo [4/6] 启动服务器...
start "" "%PROJECT_DIR%\fakemby.exe"
timeout /t 3 /nobreak >nul

REM 检查服务器是否启动成功
setlocal enabledelayedexpansion
set MAX_RETRIES=10
set RETRY_COUNT=0

:check_server
curl -s http://localhost:8096/emby/System/Info/Public >nul 2>&1
if !errorlevel! equ 0 (
    echo   ✓ 服务器已启动
    goto import_data
)

set /a RETRY_COUNT+=1
if !RETRY_COUNT! lss !MAX_RETRIES! (
    timeout /t 1 /nobreak >nul
    goto check_server
)

echo   ✗ 服务器启动失败，请检查日志
pause
exit /b 1

:import_data
echo.
echo [5/6] 导入测试数据...

REM 检查导入脚本是否存在
if not exist "%PROJECT_DIR%\import_test_data.sh" (
    echo   注意: import_test_data.sh 不存在，尝试使用 Python 版本...
    if exist "%PROJECT_DIR%\import_test_data.py" (
        python import_test_data.py
        if !errorlevel! neq 0 (
            echo   ⚠ Python 脚本执行失败
        ) else (
            echo   ✓ 数据导入成功
        )
    ) else (
        echo   ⚠ 两个导入脚本都不存在
    )
) else (
    REM 用 bash 运行导入脚本（需要 Git Bash 或 WSL）
    bash import_test_data.sh
    if !errorlevel! neq 0 (
        echo   ⚠ 数据导入失败，请手动运行
        echo   可以在浏览器中访问服务器进行手动配置
    ) else (
        echo   ✓ 数据导入成功
    )
)

echo.
echo [6/6] 配置信息
echo ════════════════════════════════════════════════════════════════
echo.
echo ✓ 服务器已启动！
echo.
echo 📊 服务器信息:
echo   地址:       http://localhost:8096
echo   默认用户:   admin
echo   默认密码:   admin
echo.
echo 🎬 测试数据:
echo   - 电影库: Big Buck Bunny (1080p + 480p)
echo   - 电视剧库: Test Series (Season 1, 2 episodes)
echo.
echo 🔗 快速链接:
echo   系统信息:   http://localhost:8096/emby/System/Info/Public
echo   浏览器打开: http://localhost:8096
echo.
echo 💡 客户端连接:
echo   小幻影视 / SenPlayer:
echo   1. 打开应用
echo   2. 添加服务器: http://localhost:8096
echo   3. 用户: admin
echo   4. 密码: admin
echo.
echo 🧪 API 测试:
echo   获取 Token:
echo   curl -X POST http://localhost:8096/emby/Users/AuthenticateByName ^
echo     -H "Content-Type: application/json" ^
echo     -d "{\"Username\":\"admin\",\"Pw\":\"admin\"}"
echo.
echo 📖 更多信息:
echo   查看 README.md 了解完整 API 文档
echo   查看 TESTING.md 了解测试指南
echo.
echo 按任意键关闭此窗口...
echo ════════════════════════════════════════════════════════════════
pause

endlocal
