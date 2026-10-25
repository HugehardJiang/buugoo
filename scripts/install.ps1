# Buugoo（布谷）一键安装 / 更新脚本（Windows）
#
#   irm https://github.com/HugehardJiang/buugoo/releases/latest/download/install.ps1 | iex
#
# 带参数运行：
#   & ([scriptblock]::Create((irm https://github.com/HugehardJiang/buugoo/releases/latest/download/install.ps1))) -Version v0.1.0 -Yes
#
# 参数：
#   -Version <v>      安装指定版本（默认最新版本）
#   -InstallDir <dir> 安装目录（默认 %LOCALAPPDATA%\Programs\buugoo）
#   -Mirror <url>     下载镜像地址（也可用环境变量 BUUGOO_MIRROR）
#   -Yes              非交互模式
#   -Force            即使已是该版本也重新安装
#
# 重复执行是安全的：已安装时转为调用 buugoo update 完成更新，已存在的配置文件不会被覆盖。
[CmdletBinding()]
param(
  [string]$Version = "",
  [string]$InstallDir = "",
  [string]$Mirror = $env:BUUGOO_MIRROR,
  [switch]$Yes,
  [switch]$Force
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$Repo = "HugehardJiang/buugoo"
function Info($m) { Write-Host "==> $m" }
function Ok($m) { Write-Host "OK  $m" -ForegroundColor Green }
function Die($m) { Write-Host "错误：$m" -ForegroundColor Red; exit 1 }

if (-not [Environment]::Is64BitOperatingSystem) { Die "只支持 64 位 Windows" }
$Arch = "amd64"
if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { Die "暂未提供 Windows ARM64 版本" }

if (-not $InstallDir) { $InstallDir = Join-Path $env:LOCALAPPDATA "Programs\buugoo" }
$ConfigDir = Join-Path $env:APPDATA "buugoo"
$DataDir = Join-Path $env:LOCALAPPDATA "buugoo"
$Bin = Join-Path $InstallDir "buugoo.exe"
$ConfigFile = Join-Path $ConfigDir "config.yaml"
$Manifest = Join-Path $ConfigDir "install.json"
if ($Mirror) { $Mirror = $Mirror.TrimEnd("/") }

# ---------- 版本 ----------
if (-not $Version) {
  if ($Mirror) {
    try {
      # 没有扩展名的文件可能以 application/octet-stream 返回，Content 是字节数组，需要手动解码。
      $content = (Invoke-WebRequest -UseBasicParsing "$Mirror/latest").Content
      if ($content -is [byte[]]) { $content = [Text.Encoding]::UTF8.GetString($content) }
      $Version = "$content".Trim()
    } catch { Die "无法从镜像 $Mirror 获取最新版本：$($_.Exception.Message)" }
  } else {
    try {
      $req = [Net.HttpWebRequest]::Create("https://github.com/$Repo/releases/latest")
      $req.AllowAutoRedirect = $false
      $resp = $req.GetResponse()
      $loc = $resp.Headers["Location"]
      $resp.Close()
      $Version = $loc.Substring($loc.LastIndexOf("/tag/") + 5)
    } catch { Die "无法获取最新版本（可用 -Mirror 指定镜像，或用 -Version 指定版本）" }
  }
}
if (-not $Version.StartsWith("v")) { $Version = "v$Version" }
if ($Version -notmatch '^v\d+\.\d+\.\d+') { Die "版本号无效：$Version" }
Info "布谷 Buugoo 安装程序（windows/$Arch），目标版本 $Version"

function AssetUrl($name) {
  if ($Mirror) { return "$Mirror/$Version/$name" }
  return "https://github.com/$Repo/releases/download/$Version/$name"
}

# ---------- 已安装：转为更新 ----------
if ((Test-Path $Bin) -and (Test-Path $Manifest) -and -not $Force) {
  $current = (& $Bin version --short 2>$null)
  if ($current -eq $Version) {
    Ok "已安装 $Version，无需更新"
    exit 0
  }
  if ($current) {
    Info "检测到已安装 $current，通过 buugoo update 更新到 $Version"
    $updateArgs = @("update", "--version", $Version, "--config", $ConfigFile, "-y")
    if ($Mirror) { $updateArgs += @("--mirror", $Mirror) }
    & $Bin @updateArgs
    if ($LASTEXITCODE -ne 0) { Die "更新失败（已自动回滚）" }
    Ok "更新完成"
    exit 0
  }
}

# ---------- 下载与校验 ----------
$Asset = "buugoo_$($Version.TrimStart('v'))_windows_$Arch.zip"
$Tmp = Join-Path ([IO.Path]::GetTempPath()) ("buugoo-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $Tmp | Out-Null
try {
  Info "下载 $(AssetUrl $Asset)"
  Invoke-WebRequest -UseBasicParsing (AssetUrl $Asset) -OutFile (Join-Path $Tmp $Asset)
  Invoke-WebRequest -UseBasicParsing (AssetUrl "checksums.txt") -OutFile (Join-Path $Tmp "checksums.txt")
  $want = $null
  foreach ($line in Get-Content (Join-Path $Tmp "checksums.txt")) {
    $parts = $line -split '\s+'
    if ($parts.Length -ge 2 -and $parts[1].TrimStart('*') -eq $Asset) { $want = $parts[0].ToLower() }
  }
  if (-not $want) { Die "校验文件中没有 $Asset，已中止" }
  $got = (Get-FileHash -Algorithm SHA256 (Join-Path $Tmp $Asset)).Hash.ToLower()
  if ($want -ne $got) { Die "SHA256 校验失败，文件可能被篡改或下载不完整，已中止安装（期望 $want，实际 $got）" }
  Ok "SHA256 校验通过"
  Expand-Archive -Path (Join-Path $Tmp $Asset) -DestinationPath (Join-Path $Tmp "x") -Force
  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  Copy-Item (Join-Path $Tmp "x\buugoo.exe") "$Bin.new" -Force
  if (Test-Path $Bin) { Move-Item $Bin "$Bin.old" -Force }
  Move-Item "$Bin.new" $Bin -Force
  Remove-Item "$Bin.old" -Force -ErrorAction SilentlyContinue
  Ok "已安装 $Bin"
} finally {
  Remove-Item -Recurse -Force $Tmp -ErrorAction SilentlyContinue
}

# ---------- 配置与数据 ----------
New-Item -ItemType Directory -Force -Path $ConfigDir, $DataDir | Out-Null
if (Test-Path $ConfigFile) {
  Ok "保留已有配置 $ConfigFile"
} else {
  & $Bin init --config $ConfigFile --data-dir $DataDir --scope user | Out-Null
  if ($LASTEXITCODE -ne 0) { Die "生成配置失败" }
  Ok "已生成配置 $ConfigFile"
}
& $Bin migrate --config $ConfigFile | Out-Null
if ($LASTEXITCODE -ne 0) { Die "数据库迁移失败" }
Ok "数据库已就绪"

# ---------- PATH ----------
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (-not ($userPath -split ';' | Where-Object { $_ -eq $InstallDir })) {
  [Environment]::SetEnvironmentVariable("Path", ($userPath.TrimEnd(';') + ";" + $InstallDir), "User")
  Ok "已把 $InstallDir 加入用户 PATH（新开的终端生效）"
}

# ---------- 安装清单 ----------
$now = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$installedAt = $now
if (Test-Path $Manifest) {
  try { $installedAt = (Get-Content $Manifest -Raw | ConvertFrom-Json).installed_at } catch {}
}
[ordered]@{
  scope = "user"; bin_dir = $InstallDir; config_dir = $ConfigDir; data_dir = $DataDir; binary = $Bin
  service = $false; service_user = ""; version = $Version; mirror = "$Mirror"
  installed_at = $installedAt; updated_at = $now
} | ConvertTo-Json | Set-Content -Encoding UTF8 $Manifest

Write-Host ""
Write-Host "布谷 $Version 安装完成"
Write-Host "  程序    $Bin"
Write-Host "  配置    $ConfigFile"
Write-Host "  数据    $DataDir"
Write-Host "  地址    http://127.0.0.1:8080"
Write-Host ""
Write-Host "下一步："
Write-Host "  1. 编辑配置文件，填写 llm.chat.api_key 与 llm.embedding.api_key"
Write-Host "  2. 启动服务：buugoo serve"
Write-Host "  3. 浏览器打开上面的地址，或在终端运行 buugoo chat"
Write-Host "  更新：buugoo update    卸载：buugoo uninstall"
