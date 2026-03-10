# openclawder installer script for Windows
# Usage: irm https://raw.githubusercontent.com/orsi-bit/openclawder/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

$Repo = "orsi-bit/openclawder"
$InstallDir = if ($env:OPENCLAWDER_INSTALL_DIR) { $env:OPENCLAWDER_INSTALL_DIR } else { "$env:LOCALAPPDATA\openclawder" }

function Get-Architecture {
    switch ($env:PROCESSOR_ARCHITECTURE) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        default { return "unsupported" }
    }
}

function Get-LatestVersion {
    $response = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
    return $response.tag_name
}

function Install-Openclawder {
    Write-Host "Installing openclawder..." -ForegroundColor Cyan

    $arch = Get-Architecture
    if ($arch -eq "unsupported") {
        Write-Host "Error: Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" -ForegroundColor Red
        Write-Host "Please install manually from https://github.com/$Repo/releases"
        exit 1
    }

    $version = Get-LatestVersion
    if (-not $version) {
        Write-Host "Error: Could not determine latest version" -ForegroundColor Red
        exit 1
    }

    Write-Host "  OS: windows"
    Write-Host "  Arch: $arch"
    Write-Host "  Version: $version"

    $binary = "openclawder-windows-$arch.exe"
    $url = "https://github.com/$Repo/releases/download/$version/$binary"

    Write-Host "  Downloading from: $url"

    # Create install directory
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    $dest = Join-Path $InstallDir "openclawder.exe"

    # Download binary
    Invoke-WebRequest -Uri $url -OutFile $dest -UseBasicParsing

    Write-Host ""
    Write-Host "Installed openclawder to $dest" -ForegroundColor Green

    # Check if install dir is in PATH
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($userPath -notlike "*$InstallDir*") {
        Write-Host ""
        Write-Host "Adding $InstallDir to your PATH..." -ForegroundColor Yellow

        $newPath = "$userPath;$InstallDir"
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")

        # Update current session
        $env:Path = "$env:Path;$InstallDir"

        Write-Host "Added to PATH. Restart your terminal for changes to take effect." -ForegroundColor Yellow
    }

    Write-Host ""
    Write-Host "Run 'openclawder setup' to configure your AI coding tool." -ForegroundColor Cyan
}

Install-Openclawder
