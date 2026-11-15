# Windows에서 직접 빌드 (Go 1.25 이상). 사용: powershell -File scripts\build.ps1
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
New-Item -ItemType Directory -Force -Path dist | Out-Null
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-X main.version=v1.0.0" -o dist\tacit-qa-demo.exe .
Write-Host "완료: dist\tacit-qa-demo.exe"
