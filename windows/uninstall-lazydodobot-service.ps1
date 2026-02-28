# ======================================
# Configuration
# ======================================

$ServiceName = "LazyDodoBot"

# ======================================
# Safety Checks
# ======================================

$existingService = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue

if (-not $existingService) {
    Write-Host "Service is not installed."
    exit 0
}

# ======================================
# Stop + Remove Existing Service
# ======================================

Write-Host "Stopping service..."

if ($existingService.Status -ne "Stopped") {
    Stop-Service $ServiceName -Force
    $existingService.WaitForStatus("Stopped", "00:00:30")
}

Write-Host "Deleting service..."
sc.exe delete $ServiceName | Out-Null

Write-Host ""
Write-Host "Service uninstalled successfully."
Write-Host ""
Write-Host "You may now delete the application folder manually."