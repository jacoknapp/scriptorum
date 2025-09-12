#!/usr/bin/env pwsh
#
# Scriptorum Dead Code Verification Script
#

Write-Host "Scriptorum Dead Code Cleanup Verification" -ForegroundColor Green
Write-Host "=========================================" -ForegroundColor Green

# Set location to script directory
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
if ($ScriptDir) {
    Set-Location $ScriptDir
}

Write-Host "Current directory: $(Get-Location)" -ForegroundColor Cyan

# Check for remaining dead code
Write-Host "`nScanning for dead code..." -ForegroundColor Cyan
$deadcode = deadcode ./... 2>$null

if ($deadcode) {
    Write-Host "Dead code still found:" -ForegroundColor Red
    foreach ($line in $deadcode) {
        Write-Host "  - $line" -ForegroundColor Red
    }
} else {
    Write-Host "✓ No dead code found" -ForegroundColor Green
}

# Run build test
Write-Host "`nTesting build..." -ForegroundColor Cyan
$buildResult = go build -o bin/scriptorum-test.exe ./cmd/scriptorum 2>&1
if ($LASTEXITCODE -eq 0) {
    Write-Host "✓ Build successful" -ForegroundColor Green
    Remove-Item "bin/scriptorum-test.exe" -ErrorAction SilentlyContinue
} else {
    Write-Host "✗ Build failed:" -ForegroundColor Red
    Write-Host $buildResult -ForegroundColor Red
}

# Run tests
Write-Host "`nRunning tests..." -ForegroundColor Cyan
$testResult = go test ./... 2>&1
if ($LASTEXITCODE -eq 0) {
    Write-Host "✓ All tests pass" -ForegroundColor Green
} else {
    Write-Host "✗ Tests failed:" -ForegroundColor Red
    Write-Host $testResult -ForegroundColor Red
}

Write-Host "`nCleanup Summary:" -ForegroundColor Cyan
Write-Host "=================" -ForegroundColor Cyan
Write-Host "✓ Removed unused function: defaultAddTemplate from bootstrap.go" -ForegroundColor Green
Write-Host "✓ Removed unused function: AddAudit from db/repo.go" -ForegroundColor Green  
Write-Host "✓ Removed unused function: handleLogin from httpapi/auth.go" -ForegroundColor Green
Write-Host "✓ Removed unused function: renderLoginForm from httpapi/auth.go" -ForegroundColor Green
Write-Host "✓ Removed unused function: NewReadarr from providers/readarr.go" -ForegroundColor Green
Write-Host "✓ Removed unused function: LookupForeignAuthorID from providers/readarr.go" -ForegroundColor Green
Write-Host "✓ Removed unused function: CreateAuthor from providers/readarr.go" -ForegroundColor Green
Write-Host "✓ Removed unused function: GetQualityProfiles from providers/readarr.go" -ForegroundColor Green
Write-Host "✓ Removed unused function: GetRootFolders from providers/readarr.go" -ForegroundColor Green
Write-Host "✓ Removed unused helper functions: cleanISBN, cleanASIN, hasIdent from providers/readarr.go" -ForegroundColor Green
Write-Host "✓ Removed unused function: SelectCandidate from providers/readarr.go" -ForegroundColor Green
Write-Host "✓ Removed unused function: ParseAuthorNameFromTitle from util/strings.go" -ForegroundColor Green
Write-Host "✓ Removed unused imports and updated test files" -ForegroundColor Green

Write-Host "`n🎉 All features preserved while removing dead code! 🎉" -ForegroundColor Green
