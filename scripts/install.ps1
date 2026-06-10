# SSH233 Agent Server — Windows installer
# Usage:
#   iwr -useb .../install.ps1 | iex
#   .\scripts\install.ps1 -FromSource

param(
    [string]$Version = "latest",
    [switch]$FromSource,
    [string]$InstallDir = "$env:LOCALAPPDATA\ssh233",
    [string]$ConfigDir = "$env:APPDATA\ssh233"
)

$ErrorActionPreference = "Stop"
$Binary = "ssh233-server"
$Repo = if ($env:SSH233_REPO) { $env:SSH233_REPO } else { "neko233-com/ssh233-agent-server" }

function Get-LatestVersion {
    try {
        $r = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
        return ($r.tag_name -replace '^[vV]', '')
    } catch { return "0.1.0" }
}

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
New-Item -ItemType Directory -Force -Path $InstallDir, "$ConfigDir\data" | Out-Null
$Target = Join-Path $InstallDir "$Binary.exe"

if ($FromSource) {
    $Root = Split-Path $PSScriptRoot -Parent
    if (-not (Test-Path (Join-Path $Root "go.mod"))) {
        throw "Run from repository: scripts/install.ps1 -FromSource"
    }
    Write-Host "Building from source: $Root"
    Push-Location $Root
    go build -o $Target ./cmd/server
    Pop-Location
} else {
    if ($Version -eq "latest") { $Version = Get-LatestVersion }
    $Version = $Version -replace '^[vV]', ''
    $Asset = "$Binary-windows-$arch.exe"
    $Url = "https://github.com/$Repo/releases/download/v$Version/$Asset"
    Write-Host "Downloading $Url"
    try {
        Invoke-WebRequest -Uri $Url -OutFile $Target -UseBasicParsing
    } catch {
        Write-Host "Release download failed — retry with: .\scripts\install.ps1 -FromSource"
        throw
    }
}

$ConfigFile = Join-Path $ConfigDir "config.yaml"
if (-not (Test-Path $ConfigFile)) {
    $Example = Join-Path (Split-Path $PSScriptRoot -Parent) "config.example.yaml"
    if (Test-Path $Example) {
        Copy-Item $Example $ConfigFile
    } else {
        @"
server:
  http_addr: ":6030"
  ssh_addr: ":2222"
database:
  driver: sqlite
  sqlite:
    path: $ConfigDir/data/ssh233.db
auth:
  jwt_secret: change-me
  token_ttl: 24h
  admin_user: root
  admin_password: root
ssh:
  host_key_path: $ConfigDir/data/host_key
agent:
  register_token: change-me
  heartbeat_ttl: 60s
"@ | Set-Content $ConfigFile -Encoding UTF8
    }
}

Write-Host ""
Write-Host "Installed: $Target"
Write-Host "Config:    $ConfigFile"
Write-Host "Start:     & '$Target' -config '$ConfigFile'"
Write-Host "Web UI:    http://127.0.0.1:6030/login.html"
Write-Host "Admin:     root / root"
Write-Host "PATH:      $InstallDir"
