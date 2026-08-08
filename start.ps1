# Teamix 一键构建 + 启动（Windows）
# 用法：右键"使用 PowerShell 运行"，或：powershell -ExecutionPolicy Bypass -File start.ps1
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$workspace = "C:\Users\21981\Desktop\GlobalProject"

Write-Host "=== 1/3 构建前端（vite → webdist-v3）==="
Set-Location "$root\web"
pnpm build
if ($LASTEXITCODE -ne 0) { Write-Host "前端构建失败" -ForegroundColor Red; exit 1 }

Write-Host "=== 2/3 构建后端（go build → teamix.exe）==="
Set-Location $root
go build -o teamix.exe ./cmd/reasonix/
if ($LASTEXITCODE -ne 0) { Write-Host "后端构建失败" -ForegroundColor Red; exit 1 }

Write-Host "=== 3/3 启动 serve（工作区: $workspace）==="
# 停掉旧进程（避免跑旧版——历史多次踩坑）
Get-Process teamix -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Milliseconds 500

Write-Host "启动 Teamix serve ..." -ForegroundColor Green
Write-Host "浏览器打开 http://localhost:8787（Ctrl+C 停止）"
& "$root\teamix.exe" serve --project $workspace
