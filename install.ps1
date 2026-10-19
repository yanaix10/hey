$ErrorActionPreference = "Stop"

$BinDir = Join-Path $HOME ".local\bin"
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null

Write-Host "[1/4] Building hey.exe and heyd.exe..."
go build -o "$BinDir\hey.exe" .\cmd\hey
go build -o "$BinDir\heyd.exe" .\cmd\heyd

Write-Host "[2/4] Adding $BinDir to User PATH..."
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$BinDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$BinDir", "User")
    $env:Path += ";$BinDir"
}

Write-Host "[3/4] Starting heyd background daemon and registering on startup..."
Stop-Process -Name "heyd" -Force -ErrorAction SilentlyContinue
Start-Process -FilePath "$BinDir\heyd.exe" -WindowStyle Hidden

$RegPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
Set-ItemProperty -Path $RegPath -Name "HeyChatDaemon" -Value "`"$BinDir\heyd.exe`""

Write-Host "[4/4] Configuring hey@username hook in PowerShell profile..."
if (!(Test-Path -Path $PROFILE)) {
    New-Item -ItemType File -Path $PROFILE -Force | Out-Null
}

$HookCode = @'
# >>> hey chat hook >>>
$ExecutionContext.InvokeCommand.CommandNotFoundAction = {
    param($CommandName, $CommandLookupEventArgs)
    if ($CommandName -match '^hey@(.+)$') {
        $TargetUser = $Matches[1]
        $CommandLookupEventArgs.CommandScriptBlock = { & hey.exe $TargetUser }.GetNewClosure()
        $CommandLookupEventArgs.StopSearch = $true
    }
}
# <<< hey chat hook <<<
'@

$CurrentProfile = Get-Content $PROFILE -Raw -ErrorAction SilentlyContinue
if ($CurrentProfile -notlike "*hey chat hook*") {
    Add-Content -Path $PROFILE -Value "`n$HookCode"
}

Write-Host "Installation complete! Restart PowerShell and type: hey@<username>"