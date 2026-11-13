[CmdletBinding()]
param(
    [ValidateSet("all", "lint", "test", "web", "build", "clean", "help")]
    [string]$Task = "help"
)

$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $RepoRoot

function Ensure-GoPath {
    if (Get-Command go -ErrorAction SilentlyContinue) {
        return
    }
    $candidates = @()
    $preferred = Join-Path $env:USERPROFILE "sdk\go1.27.3\bin"
    if (Test-Path (Join-Path $preferred "go.exe")) {
        $candidates += $preferred
    }
    $sdkRoot = Join-Path $env:USERPROFILE "sdk"
    if (Test-Path $sdkRoot) {
        $others = Get-ChildItem -Path $sdkRoot -Directory -Filter "go*" -ErrorAction SilentlyContinue |
            Sort-Object Name -Descending |
            ForEach-Object { Join-Path $_.FullName "bin" } |
            Where-Object { (Test-Path (Join-Path $_ "go.exe")) -and ($_ -ne $preferred) }
        $candidates += $others
    }
    $goUserBin = Join-Path $env:USERPROFILE "go\bin"
    if (Test-Path $goUserBin) {
        $candidates += $goUserBin
    }
    if ($candidates.Count -gt 0) {
        $env:PATH = ($candidates -join ";") + ";" + $env:PATH
    }
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw "未找到 go 命令，请确认 Go 已安装并在 PATH 或 $env:USERPROFILE\sdk\go1.27.3\bin 下"
    }
}

function Invoke-Lint {
    Ensure-GoPath
    Write-Host "==> gofmt 检查"
    $unformatted = & gofmt -l .
    if ($LASTEXITCODE -ne 0) {
        throw "gofmt 执行失败"
    }
    if ($unformatted) {
        Write-Host "以下文件未格式化:" -ForegroundColor Red
        $unformatted | ForEach-Object { Write-Host "  $_" }
        exit 1
    }
    Write-Host "==> go vet ./..."
    & go vet ./...
    if ($LASTEXITCODE -ne 0) {
        throw "go vet 检查失败"
    }
}

function Invoke-Test {
    Ensure-GoPath
    $hasGcc = [bool](Get-Command gcc -ErrorAction SilentlyContinue)
    if ($hasGcc) {
        Write-Host "==> go test -race ./... (CGO_ENABLED=1)"
        $env:CGO_ENABLED = "1"
        & go test -race ./...
    } else {
        Write-Host "==> go test ./... (未检测到 gcc，以 CGO_ENABLED=0 运行)"
        $env:CGO_ENABLED = "0"
        & go test ./...
    }
    if ($LASTEXITCODE -ne 0) {
        throw "Go 单元测试失败"
    }
}

function Invoke-Web {
    Write-Host "==> 构建前端并复制到 internal/web/dist"
    Push-Location (Join-Path $RepoRoot "web")
    try {
        if (-not (Test-Path "node_modules")) {
            & npm.cmd ci
            if ($LASTEXITCODE -ne 0) {
                throw "npm ci 失败"
            }
        }
        & npm.cmd run build
        if ($LASTEXITCODE -ne 0) {
            throw "前端构建失败"
        }
    } finally {
        Pop-Location
    }

    $distTarget = Join-Path $RepoRoot "internal\web\dist"
    if (Test-Path $distTarget) {
        Remove-Item -Path $distTarget -Recurse -Force
    }
    New-Item -ItemType Directory -Path $distTarget -Force | Out-Null
    Copy-Item -Path (Join-Path $RepoRoot "web\dist\*") -Destination $distTarget -Recurse -Force
    $gitkeep = Join-Path $distTarget ".gitkeep"
    if (-not (Test-Path $gitkeep)) {
        [System.IO.File]::WriteAllBytes($gitkeep, @())
    }
}

function Invoke-Build {
    Ensure-GoPath
    $pkg = "github.com/HugehardJiang/buugoo"
    $version = (& git describe --tags --always --dirty 2>$null)
    if ($LASTEXITCODE -ne 0 -or -not $version) { $version = "dev" }
    $commit = (& git rev-parse --short HEAD 2>$null)
    if ($LASTEXITCODE -ne 0 -or -not $commit) { $commit = "none" }
    $date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

    $ldflags = "-s -w -X $pkg/internal/buildinfo.Version=$($version.Trim()) -X $pkg/internal/buildinfo.Commit=$($commit.Trim()) -X $pkg/internal/buildinfo.Date=$date"
    New-Item -ItemType Directory -Path (Join-Path $RepoRoot "bin") -Force | Out-Null

    Write-Host "==> 构建 bin/buugoo.exe ($($version.Trim()))"
    $env:CGO_ENABLED = "0"
    & go build -trimpath -ldflags $ldflags -o (Join-Path $RepoRoot "bin\buugoo.exe") ./cmd/buugoo
    if ($LASTEXITCODE -ne 0) {
        throw "Go 构建失败"
    }
}

function Invoke-Clean {
    foreach ($dir in @("bin", "dist")) {
        $p = Join-Path $RepoRoot $dir
        if (Test-Path $p) {
            Remove-Item -Path $p -Recurse -Force
        }
    }
    $distTarget = Join-Path $RepoRoot "internal\web\dist"
    if (Test-Path $distTarget) {
        Remove-Item -Path $distTarget -Recurse -Force
    }
    New-Item -ItemType Directory -Path $distTarget -Force | Out-Null
    [System.IO.File]::WriteAllBytes((Join-Path $distTarget ".gitkeep"), @())
    Write-Host "==> 已清理构建产物"
}

function Show-Help {
    Write-Host "Buugoo Windows 本地开发脚本"
    Write-Host "用法: powershell -ExecutionPolicy Bypass -File .\scripts\dev.ps1 <任务>"
    Write-Host ""
    Write-Host "可用任务:"
    Write-Host "  all     依次执行 lint、test、web、build"
    Write-Host "  lint    执行 gofmt 检查与 go vet ./..."
    Write-Host "  test    运行 Go 单元测试"
    Write-Host "  web     构建前端并同步到 internal/web/dist"
    Write-Host "  build   构建 bin/buugoo.exe"
    Write-Host "  clean   清理 bin、dist 与嵌入前端产物"
    Write-Host "  help    显示此帮助"
}

switch ($Task) {
    "lint"  { Invoke-Lint }
    "test"  { Invoke-Test }
    "web"   { Invoke-Web }
    "build" { Invoke-Build }
    "all"   { Invoke-Lint; Invoke-Test; Invoke-Web; Invoke-Build }
    "clean" { Invoke-Clean }
    "help"  { Show-Help }
}
