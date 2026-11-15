#!/usr/bin/env sh
# 실행파일 빌드 (Go 1.25 이상). 사용: sh scripts/build.sh v1.0.0
set -eu
VERSION="${1:-v1.0.0}"
cd "$(dirname "$0")/.."
rm -rf dist && mkdir -p dist/pkg

build() {
  # $1 GOOS, $2 GOARCH, $3 실행파일 이름, $4 배포 zip 이름
  mkdir -p "dist/pkg/$4"
  GOOS="$1" GOARCH="$2" CGO_ENABLED=0 go build -trimpath \
    -ldflags "-X main.version=$VERSION" -o "dist/pkg/$4/$3" .
  cp .env.example "dist/pkg/$4/.env.example"
  cp scripts/release-readme.txt "dist/pkg/$4/README.txt"
  (cd dist/pkg && zip -qr "../$4.zip" "$4")
}

build windows amd64 tacit-qa-demo.exe tacit-qa-demo_windows_amd64
build darwin  arm64 tacit-qa-demo     tacit-qa-demo_macos_apple-silicon
build darwin  amd64 tacit-qa-demo     tacit-qa-demo_macos_intel
build linux   amd64 tacit-qa-demo     tacit-qa-demo_linux_amd64

(cd dist && sha256sum ./*.zip > SHA256SUMS.txt)
rm -rf dist/pkg
ls -la dist
