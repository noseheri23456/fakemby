# 在 Windows 上跑 go test -race。
#
# Go 的竞态检测器在 Windows 上需要 cgo，也就需要一个可用的 gcc。
# 本机往往没有把 gcc 放进 PATH（例如只有 Nuitka 缓存里那份 MinGW），
# 直接 go test -race 会报：cgo: C compiler "gcc" not found。
#
# 本脚本按以下顺序探测 gcc，找到后通过 CC 环境变量传给 Go，再跑 -race：
#   1. 已设置的 $env:CC
#   2. PATH 里的 gcc
#   3. 常见安装位置（Nuitka 缓存、TDM-GCC、MSYS2、mingw64、Strawberry、Chocolatey）
#
# 用法：
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\dev\test-race.ps1
#   .\scripts\dev\test-race.ps1 ./internal/... -v          # 额外参数原样传给 go test

param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$GoTestArgs
)

$ErrorActionPreference = "Stop"

function Find-Gcc {
    # 1. 显式指定
    if ($env:CC -and (Test-Path $env:CC)) { return (Resolve-Path $env:CC).Path }

    # 2. PATH
    $cmd = Get-Command gcc.exe -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }

    # 3. 已知安装位置
    $roots = @(
        "$env:LOCALAPPDATA\Nuitka\Nuitka\Cache\downloads\gcc",
        "$env:LOCALAPPDATA\Nuitka\Nuitka\Cache\gcc",
        "C:\TDM-GCC-64\bin",
        "C:\msys64\mingw64\bin",
        "C:\msys64\ucrt64\bin",
        "C:\mingw64\bin",
        "C:\Strawberry\c\bin",
        "C:\ProgramData\chocolatey\lib\mingw\tools\install\mingw64\bin"
    )

    foreach ($root in $roots) {
        if (-not (Test-Path $root)) { continue }
        $hit = Get-ChildItem -Path $root -Filter gcc.exe -Recurse -ErrorAction SilentlyContinue |
            Select-Object -First 1
        if ($hit) { return $hit.FullName }
    }
    return $null
}

$gcc = Find-Gcc
if (-not $gcc) {
    Write-Host ""
    Write-Host "未找到 gcc.exe。-race 在 Windows 上需要 cgo。" -ForegroundColor Red
    Write-Host "可选方案：" -ForegroundColor Yellow
    Write-Host "  1. 安装 MinGW-w64 或 TDM-GCC，并把 bin 目录加入 PATH"
    Write-Host "  2. 直接指定：  `$env:CC = 'C:\path\to\gcc.exe'"
    Write-Host "  3. 跳过竞态检测：  go test ./..."
    Write-Host ""
    exit 1
}

Write-Host "gcc: $gcc" -ForegroundColor DarkGray
$env:CC = $gcc
$env:CGO_ENABLED = "1"

if ($GoTestArgs.Count -eq 0) { $GoTestArgs = @("./...") }

& go test -race -count=1 @GoTestArgs
exit $LASTEXITCODE
