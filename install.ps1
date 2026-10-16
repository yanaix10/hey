Write-Host "Building binaries..."
go build -o hey.exe ./cmd/hey
go build -o heyd.exe ./cmd/heyd

$binDir = "$env:USERPROFILE\.local\bin"
New-Item -ItemType Directory -Force -Path $binDir | Out-Null

Stop-Process -Name "heyd" -Force -ErrorAction SilentlyContinue
Move-Item -Force hey.exe, heyd.exe $binDir

# Add ~/.local/bin to User PATH if not already present
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$binDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$binDir", "User")
    $env:Path += ";$binDir"
}

# Start daemon in background now and register in Windows Startup folder
$startupDir = "$env:APPDATA\Microsoft\Windows\Start Menu\Programs\Startup"
$vbsPath = "$startupDir\heyd-Autostart.vbs"
Set-Content -Path $vbsPath -Value "Set WshShell = CreateObject(`"WScript.Shell`")`nWshShell.Run `"$binDir\heyd.exe`", 0, False"
Start-Process "$binDir\heyd.exe" -WindowStyle Hidden

# Inject hey@username hook into PowerShell Profile
if (!(Test-Path $PROFILE)) {
    New-Item -ItemType File -Path $PROFILE -Force | Out-Null
}
$hook = @'

$ExecutionContext.InvokeCommand.CommandNotFoundAction = {
    param($CommandName,$CommandLookupEventArgs)
    if ($CommandName -match '^hey@(.+)$') {
        $target =$Matches[1]
        $CommandLookupEventArgs.CommandScriptBlock = { hey.exe $target }.GetNewClosure()
        $CommandLookupEventArgs.StopSearch =$true
    }
}
'@
if (!(Select-String -Path $PROFILE -Pattern "hey@" -Quiet)) {
    Add-Content -Path $PROFILE -Value$hook
}

Write-Host "Installation complete! Restart PowerShell and type hey@username"