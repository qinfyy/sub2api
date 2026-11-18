<#
.SYNOPSIS
    Sub2API 一键构建脚本 (支持全栈/前后端独立编译及热部署)

.DESCRIPTION
    编译 Sub2API 项目，默认包含前端构建 (Vite) 与嵌入式后端二进制编译 (Go)。
    支持通过参数控制仅编译后端、仅编译前端、部署到指定路径及自动重启服务。

.PARAMETER SkipFrontend
    跳过前端构建，直接使用现有的 backend/internal/web/dist 资源编译后端。

.PARAMETER FrontendOnly
    仅编译前端项目 (输出至 backend/internal/web/dist)。

.PARAMETER OutputDir
    编译后二进制存放目录，默认为 backend/bin。

.PARAMETER OutputName
    编译后二进制文件名，默认为 server.exe。

.PARAMETER DeployPath
    可选，编译成功后自动部署/复制的目标文件路径（例如 E:\sub2api\sub2api.exe）。

.PARAMETER Restart
    可选，配合 DeployPath 使用，部署后自动重启正在运行的 sub2api 进程。

.EXAMPLE
    .\build.ps1
    标准完整构建（编译前端 + 编译嵌入前端的后端二进制至 backend/bin/server.exe）

.EXAMPLE
    .\build.ps1 -SkipFrontend
    跳过前端构建，仅编译 Go 后端二进制

.EXAMPLE
    .\build.ps1 -DeployPath "E:\sub2api\sub2api.exe" -Restart
    构建完成后自动更新部署到 E:\sub2api 并重启服务
#>

param(
    [Alias("BackendOnly", "Quick")]
    [switch]$SkipFrontend,

    [switch]$FrontendOnly,

    [string]$OutputDir = "$PSScriptRoot\backend\bin",

    [string]$OutputName = "server.exe",

    [string]$DeployPath = "",

    [switch]$Restart
)

$ErrorActionPreference = "Stop"

$startTime = Get-Date

function Write-Step {
    param([string]$Message)
    Write-Host "`n====> $Message" -ForegroundColor Cyan
}

function Write-Success {
    param([string]$Message)
    Write-Host "[OK] $Message" -ForegroundColor Green
}

function Write-WarnMsg {
    param([string]$Message)
    Write-Host "[WARN] $Message" -ForegroundColor Yellow
}

function Write-ErrorMsg {
    param([string]$Message)
    Write-Host "[ERROR] $Message" -ForegroundColor Red
}

$rootDir = $PSScriptRoot
$frontendDir = Join-Path $rootDir "frontend"
$backendDir = Join-Path $rootDir "backend"
$distDir = Join-Path $backendDir "internal\web\dist"
$targetBinary = Join-Path $OutputDir $OutputName

Write-Host "==========================================" -ForegroundColor Magenta
Write-Host "       Sub2API 构建系统 (Windows)        " -ForegroundColor Magenta
Write-Host "==========================================" -ForegroundColor Magenta
Write-Host "项目目录: $rootDir" -ForegroundColor DarkGray
Write-Host "输出文件: $targetBinary" -ForegroundColor DarkGray

# ---------------------------------------------------------
# 1. 前端构建阶段
# ---------------------------------------------------------
if (-not $SkipFrontend) {
    Write-Step "1/2 正在构建前端 (Vite + Vue 3)..."
    
    # 检查 pnpm 是否安装
    if (-not (Get-Command pnpm -ErrorAction SilentlyContinue)) {
        Write-ErrorMsg "未找到 pnpm，请确保已安装 Node.js 和 pnpm！"
        exit 1
    }

    $frontendStart = Get-Date
    Push-Location $frontendDir
    try {
        pnpm run build --config.verify-deps-before-run=false
        if ($LASTEXITCODE -ne 0) {
            Write-WarnMsg "pnpm run build 失败，尝试使用 npx vite build 直接构建..."
            npx vite build
            if ($LASTEXITCODE -ne 0) {
                throw "前端构建失败，退出代码: $LASTEXITCODE"
            }
        }
    } finally {
        Pop-Location
    }

    $frontendDuration = ((Get-Date) - $frontendStart).TotalSeconds
    if (Test-Path (Join-Path $distDir "index.html")) {
        Write-Success ("前端构建完成，已输出至 backend/internal/web/dist (耗时: {0:N1} 秒)" -f $frontendDuration)
    } else {
        throw "前端构建产物未在 $distDir 中找到 index.html！"
    }

    if ($FrontendOnly) {
        $totalDuration = ((Get-Date) - $startTime).TotalSeconds
        Write-Host ""
        Write-Success ("前端单模块构建成功！(总耗时: {0:N1} 秒)" -f $totalDuration)
        exit 0
    }
} else {
    Write-WarnMsg "已跳过前端构建，将直接使用现有 dist 目录嵌入编译。"
    if (-not (Test-Path (Join-Path $distDir "index.html"))) {
        Write-WarnMsg "注意: $distDir 中未找到 index.html，编译后的二进制可能无法访问静态前端！"
    }
}

# ---------------------------------------------------------
# 2. 后端构建阶段
# ---------------------------------------------------------
Write-Step "2/2 正在编译 Go 后端二进制 (含嵌入式前端)..."

# 检查 go 是否安装
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-ErrorMsg "未找到 go 命令，请确认 Go 编译器已加入 PATH！"
    exit 1
}

# 解析版本信息
$version = "0.2.16"
$versionFile = Join-Path $backendDir "cmd\server\VERSION"
if (Test-Path $versionFile) {
    $version = (Get-Content $versionFile -Raw).Trim()
}

$commit = "unknown"
if (Get-Command git -ErrorAction SilentlyContinue) {
    try {
        $gitCommit = git rev-parse --short HEAD 2>$null
        if ($gitCommit) { $commit = $gitCommit.Trim() }
    } catch {}
}

$buildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$ldflags = "-s -w -X main.Version=$version -X main.Commit=$commit -X main.Date=$buildDate"

Write-Host "构建参数: Version=$version, Commit=$commit, Date=$buildDate" -ForegroundColor DarkGray

if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
}

$backendStart = Get-Date
Push-Location $backendDir
try {
    # 强制开启 embed 标签，注入版本号并裁剪调试符号
    go build -tags embed -ldflags "$ldflags" -trimpath -o "$targetBinary" ./cmd/server
    if ($LASTEXITCODE -ne 0) {
        throw "Go 后端编译失败，退出代码: $LASTEXITCODE"
    }
} finally {
    Pop-Location
}

$backendDuration = ((Get-Date) - $backendStart).TotalSeconds

if (Test-Path $targetBinary) {
    $fileItem = Get-Item $targetBinary
    $sizeMB = $fileItem.Length / 1MB
    Write-Success ("后端编译成功: {0} ({1:N2} MB, 耗时: {2:N1} 秒)" -f $targetBinary, $sizeMB, $backendDuration)
} else {
    throw "未找到构建目标文件: $targetBinary"
}

# ---------------------------------------------------------
# 3. 部署与服务重启 (可选)
# ---------------------------------------------------------
if ($DeployPath) {
    Write-Step "3/3 部署到目标路径: $DeployPath ..."
    
    $deployDir = Split-Path -Path $DeployPath -Parent
    if (-not (Test-Path $deployDir)) {
        New-Item -ItemType Directory -Path $deployDir -Force | Out-Null
    }

    # 如果需要重启正在运行的进程
    $stoppedProcesses = @()
    if ($Restart) {
        $exeName = [System.IO.Path]::GetFileNameWithoutExtension($DeployPath)
        $runningProcesses = Get-Process -Name $exeName -ErrorAction SilentlyContinue
        foreach ($proc in $runningProcesses) {
            Write-Host "正在停止运行中的进程 $($proc.Name) (PID: $($proc.Id))..." -ForegroundColor Yellow
            Stop-Process -Id $proc.Id -Force
            $stoppedProcesses += $proc
        }
        if ($stoppedProcesses.Count -gt 0) {
            Start-Sleep -Seconds 1
        }
    }

    # 备份原有文件
    if (Test-Path $DeployPath) {
        $backupPath = "$DeployPath.bak"
        Copy-Item -Path $DeployPath -Destination $backupPath -Force
        Write-Host "已备份原二进制至 $backupPath" -ForegroundColor DarkGray
    }

    # 复制新二进制
    Copy-Item -Path $targetBinary -Destination $DeployPath -Force
    Write-Success "成功部署到 $DeployPath"

    # 如果指定了重启，拉起服务
    if ($Restart) {
        Write-Host "正在重新拉起服务进程..." -ForegroundColor Cyan
        $zoneZip = Join-Path $deployDir "zoneinfo.zip"
        $zoneDir = Join-Path $deployDir "zoneinfo"
        if (Test-Path $zoneZip) {
            $env:ZONEINFO = $zoneZip
        } elseif (Test-Path $zoneDir) {
            $env:ZONEINFO = $zoneDir
        }
        if (-not $env:TZ) {
            $env:TZ = "Asia/Shanghai"
        }
        $newProc = Start-Process -FilePath $DeployPath -WorkingDirectory $deployDir -PassThru
        Start-Sleep -Seconds 1
        if (-not $newProc.HasExited) {
            Write-Success ("服务已成功拉起 (新 PID: $($newProc.Id))")
        } else {
            Write-WarnMsg "进程拉起后已退出，请检查日志。"
        }
    }
}

$totalTime = ((Get-Date) - $startTime).TotalSeconds
Write-Host "`n==========================================" -ForegroundColor Green
Write-Host ("  构建全部完成！总耗时: {0:N1} 秒" -f $totalTime) -ForegroundColor Green
Write-Host "==========================================" -ForegroundColor Green
