# 一键构建：vue3 前端 + Go 后端 -> teamix.exe（前端自动嵌入）
# 用法: powershell -ExecutionPolicy Bypass -File .\build.ps1
$ErrorActionPreference = "Stop"
$root = $PSScriptRoot

Write-Host "[1/3] 构建 vue3 前端 (npm run build)..." -ForegroundColor Cyan
Set-Location "$root\web"
if (-not (Test-Path "node_modules\.bin\vite.cmd")) {
    npm install
}
npm run build
if ($LASTEXITCODE -ne 0) { throw "前端构建失败" }

Write-Host "[2/3] 同步构建产物到 internal\serve\webdist-v3..." -ForegroundColor Cyan
$dst = "$root\internal\serve\webdist-v3"
Remove-Item "$dst\assets\*" -Force -ErrorAction SilentlyContinue
Copy-Item "$root\web\dist-v3\index.html" "$dst\index.html" -Force
Copy-Item "$root\web\dist-v3\assets\*" "$dst\assets\" -Force
Copy-Item "$root\web\dist-v3\favicon.svg", "$root\web\dist-v3\icons.svg" "$dst\" -Force -ErrorAction SilentlyContinue

Write-Host "[3/3] 编译 teamix.exe..." -ForegroundColor Cyan
Set-Location $root
go build -o teamix.exe ./cmd/reasonix/
if ($LASTEXITCODE -ne 0) { throw "Go 编译失败" }

Write-Host ""
Write-Host "构建完成! 启动: .\teamix.exe serve --teamix --addr :8787" -ForegroundColor Green
