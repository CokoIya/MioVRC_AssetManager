#!/bin/sh
# Cross-build on Linux / WSL: exe + installer + portable zip into dist/.
# Needs Go 1.24+ and NSIS 3 (makensis). Run from the repository root.
set -e
VERSION=$(sed -n 's/^var AppVersion = "\([^"]*\)".*/\1/p' internal/core/version.go)
APP=MioVRC_AssetManager
CN=MioVRCA
mkdir -p dist
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H windowsgui -s -w" -o "dist/$APP.exe" ./cmd/miovrca
cp packaging/installer.nsi packaging/使用说明.txt cmd/miovrca/app.ico dist/
(cd dist && LC_ALL=C.UTF-8 makensis -INPUTCHARSET UTF8 installer.nsi && rm installer.nsi app.ico)
rm -rf dist/port && mkdir -p "dist/port/$APP" && cp "dist/$APP.exe" "dist/使用说明.txt" "dist/port/$APP/"
# (not with zip: it leaves the name of 使用说明.txt unmarked, and Windows Explorer then shows it garbled)
go run ./packaging/mkzip "dist/$APP-portable-$VERSION.zip" "dist/port/$APP" && rm -rf dist/port
# the release assets are dist/$APP-setup-x.exe and dist/$APP-portable-x.zip;
# copies with Chinese names for passing around
mkdir -p dist/share
cp "dist/$APP-setup-$VERSION.exe" "dist/share/$CN-安装-$VERSION.exe"
cp "dist/$APP-portable-$VERSION.zip" "dist/share/$CN-便携版-$VERSION.zip"
echo "dist/: $(ls dist | tr '\n' ' ')"
