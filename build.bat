@echo off
chcp 65001 >nul
cd /d "%~dp0"
rem 需要 Go 1.24 或更新版本：https://go.dev/dl/
go test . || exit /b 1
if not exist dist mkdir dist
go build -trimpath -ldflags "-H windowsgui -s -w" -o "dist\MioVRC_AssetManager.exe" . || exit /b 1
echo 已生成 dist\MioVRC_AssetManager.exe
