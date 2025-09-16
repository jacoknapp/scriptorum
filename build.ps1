# PowerShell build script for Scriptorum
# Usage: .\build.ps1 [target]
# Available targets: build, test, run, clean

param(
    [string]$Target = "build"
)

$AppName = "scriptorum"
$BinDir = "bin"
$OutFile = "$BinDir\$AppName.exe"

function Build-App {
    Write-Host "Building $AppName..."
    if (!(Test-Path $BinDir)) {
        New-Item -ItemType Directory -Path $BinDir | Out-Null
    }
    go build -o $OutFile ./cmd/scriptorum
    if ($LASTEXITCODE -eq 0) {
        Write-Host "Build successful: $OutFile" -ForegroundColor Green
    } else {
        Write-Host "Build failed" -ForegroundColor Red
        exit $LASTEXITCODE
    }
}

function Test-App {
    Write-Host "Running tests..."
    go test ./...
    if ($LASTEXITCODE -eq 0) {
        Write-Host "Tests passed" -ForegroundColor Green
    } else {
        Write-Host "Tests failed" -ForegroundColor Red
        exit $LASTEXITCODE
    }
}

function Run-App {
    if (!(Test-Path $OutFile)) {
        Write-Host "Binary not found, building first..."
        Build-App
    }
    Write-Host "Running $AppName..."
    & $OutFile
}

function Clean-Build {
    Write-Host "Cleaning build artifacts..."
    if (Test-Path $BinDir) {
        Remove-Item -Path $BinDir -Recurse -Force
        Write-Host "Cleaned build directory" -ForegroundColor Green
    }
}

switch ($Target.ToLower()) {
    "build" { Build-App }
    "test" { Test-App }
    "run" { Run-App }
    "clean" { Clean-Build }
    default {
        Write-Host "Usage: .\build.ps1 [build|test|run|clean]"
        Write-Host "Available targets:"
        Write-Host "  build - Build the application (default)"
        Write-Host "  test  - Run tests"
        Write-Host "  run   - Build and run the application"
        Write-Host "  clean - Clean build artifacts"
    }
}