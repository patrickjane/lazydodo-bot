# ======================================
# Configuration
# ======================================

$ServiceName = "LazyDodoBot"
$DisplayName = "RCON-Discord LazyDodo bot"
$Description = "Sends player information retrieved via RCON to discord"

# Automatically detect install directory
$InstallDir = $PSScriptRoot

$ExePath    = Join-Path $InstallDir "lazydodobot.windows-amd64.exe"
$ConfigPath = Join-Path $InstallDir "config.json"

# Properly quoted binary path
$BinaryPath = "`"$ExePath`" --config-file `"$ConfigPath`""

# ======================================
# Safety Checks
# ======================================

if (-not (Test-Path $ExePath)) {
    Write-Error "Executable not found at $ExePath"
    exit 1
}

if (-not (Test-Path $ConfigPath)) {
    Write-Error "Config file not found at $ConfigPath"
    exit 1
}

# ======================================
# Stop + Remove Existing Service
# ======================================

$existingService = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue

if ($existingService) {

    Write-Host "Service already exists. Stopping..."

    if ($existingService.Status -ne "Stopped") {
        Stop-Service $ServiceName -Force
        $existingService.WaitForStatus("Stopped", "00:00:30")
    }

    Write-Host "Deleting existing service..."
    sc.exe delete $ServiceName | Out-Null

    Start-Sleep -Seconds 2
}

# ======================================
# Create Service
# ======================================

Write-Host "Creating Windows Service..."

New-Service `
    -Name $ServiceName `
    -BinaryPathName $BinaryPath `
    -DisplayName $DisplayName `
    -Description $Description `
    -StartupType Automatic

# Restart automatically on crash
sc.exe failure $ServiceName reset= 0 actions= restart/5000 | Out-Null

# ======================================
# Start Service
# ======================================

Write-Host "Starting service..."
Start-Service $ServiceName

Write-Host ""
Write-Host "Service installed or updated successfully."
Write-Host "   Installed from: $InstallDir"
Write-Host "You can safely re-run this script to apply config changes."