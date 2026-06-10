$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
go test ./... -count=1 -cover @args
