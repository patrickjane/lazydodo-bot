#!/usr/bin/env bash

set -e

APP_NAME="lazydodobot"

# Versionsnummer prüfen
if [ -z "$BUILD_VERSION" ]; then
  echo "ERROR: BUILD_VERSION ist nicht gesetzt."
  echo "Beispiel: BUILD_VERSION=1.2.3 ./build.sh"
  exit 1
fi

LDFLAGS="-X 'main.version=${BUILD_VERSION}'"

echo "Building ${APP_NAME} Version ${BUILD_VERSION}..."

# Linux amd64
echo "-> linux/amd64"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
go build -ldflags "${LDFLAGS}" \
-o ${APP_NAME}.linux-amd64 \
cmd/lazydodobot/main.go

# Linux arm64
echo "-> linux/arm64"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
go build -ldflags "${LDFLAGS}" \
-o ${APP_NAME}.linux-arm64 \
cmd/lazydodobot/main.go

# macOS amd64 (Intel)
echo "-> darwin/amd64"
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 \
go build -ldflags "${LDFLAGS}" \
-o ${APP_NAME}.darwin-amd64 \
cmd/lazydodobot/main.go

# macOS arm64 (Apple Silicon)
echo "-> darwin/arm64"
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
go build -ldflags "${LDFLAGS}" \
-o ${APP_NAME}.darwin-arm64 \
cmd/lazydodobot/main.go

# Windows amd64
echo "-> windows/amd64"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
go build -ldflags "${LDFLAGS}" \
-o ${APP_NAME}.windows-amd64.exe \
cmd/lazydodobot/main-windows.go

echo "Done ✅"
