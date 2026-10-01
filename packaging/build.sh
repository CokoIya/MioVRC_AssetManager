#!/bin/sh
# Cross-build on Linux / WSL: exe + installer + portable zip into dist/.
# Needs Go 1.24+, NSIS 3 (makensis) and zip. Run from the repository root.
set -e
VERSION=$(sed -n 's/^const appVersion = "\(.*\)"/\1/p' main.go)
mkdir -p dist
go test .
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H windowsgui -s -w" -o "dist/VRC素材库.exe" .
cp packaging/installer.nsi packaging/使用说明.txt app.ico dist/
(cd dist && LC_ALL=C.UTF-8 makensis -INPUTCHARSET UTF8 installer.nsi && rm installer.nsi app.ico)
rm -rf dist/port && mkdir -p "dist/port/VRC素材库" && cp "dist/VRC素材库.exe" "dist/使用说明.txt" "dist/port/VRC素材库/"
(cd dist/port && zip -qr "../VRC素材库-便携版-$VERSION.zip" "VRC素材库") && rm -rf dist/port
echo "dist/: $(ls dist | tr '\n' ' ')"
