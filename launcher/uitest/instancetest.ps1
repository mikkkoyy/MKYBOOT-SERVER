# MKYBOOT Launcher - single-instance integration test.
#
# Proves cross-process exclusion, which CANNOT be proven from inside one Go
# process: Windows mutexes are recursive per OS thread and the Go runtime
# reuses OS threads, so in-process goroutine tests give false passes.
# Launching the real executable is the only faithful check.
#
# Usage (from launcher/):
#   powershell -NoProfile -ExecutionPolicy Bypass -File "./uitest/instancetest.ps1"
#   powershell ... -File "./uitest/instancetest.ps1" -Exe "dist\MKYBOOT Launcher.exe"
#
# Exits 0 when every check passes, 1 otherwise.

param(
    [string]$Exe = ""
)

$ErrorActionPreference = "Continue"

$script:Failures = 0
function Report([string]$name, [bool]$ok, [string]$detail = "") {
    if ($ok) { Write-Host ("  PASS  " + $name) }
    else {
        Write-Host ("  FAIL  " + $name)
        if ($detail -ne "") { Write-Host ("          " + $detail) }
        $script:Failures++
    }
}

if ($Exe -eq "") { $Exe = Join-Path $PSScriptRoot "..\dist\MKYBOOT Launcher.exe" }
$Exe = [System.IO.Path]::GetFullPath($Exe)

Write-Host "=============================================="
Write-Host " MKYBOOT Launcher - single-instance test"
Write-Host "=============================================="
Write-Host "exe: $Exe"

if (-not (Test-Path -LiteralPath $Exe)) {
    Write-Host "FAIL: executable not found. Build it first:"
    Write-Host "  go build -trimpath -ldflags `"-s -w -H windowsgui`" -o `"dist/MKYBOOT Launcher.exe`" ."
    exit 1
}

function Count-Inst {
    return @(Get-Process -Name "MKYBOOT Launcher" -ErrorAction SilentlyContinue).Count
}

function Clear-Inst {
    Get-Process -Name "MKYBOOT Launcher" -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Milliseconds 900
}

Write-Host ""
Write-Host "-- preflight --"
Clear-Inst
Report "no launcher running before the test" ((Count-Inst) -eq 0) ("found " + (Count-Inst))

# --- 1. the first launch must survive ------------------------------------
Write-Host ""
Write-Host "-- 1. first instance --"
$p1 = Start-Process -FilePath $Exe -PassThru
Start-Sleep -Seconds 3
$c1 = Count-Inst
Report "first launch starts and stays" ($c1 -eq 1) ("instances=" + $c1)

# --- 2. a second launch must exit by itself ------------------------------
Write-Host ""
Write-Host "-- 2. second instance exits cleanly --"
$p2 = Start-Process -FilePath $Exe -PassThru
$exited = $p2.WaitForExit(10000)
Start-Sleep -Milliseconds 900
$c2 = Count-Inst
Report "second launch exits by itself" $exited ("still running after 10s")
if ($exited) {
    Report "second launch exit code is 0" ($p2.ExitCode -eq 0) ("exitcode=" + $p2.ExitCode)
}
Report "no duplicate instance created" ($c2 -eq 1) ("instances=" + $c2)

# --- 3. a rapid burst must still yield exactly one ------------------------
Write-Host ""
Write-Host "-- 3. burst of 8 simultaneous launches --"
Clear-Inst
$burst = @()
1..8 | ForEach-Object { $burst += Start-Process -FilePath $Exe -PassThru }
Start-Sleep -Seconds 6
$c3 = Count-Inst
Report "burst of 8 yields exactly one instance" ($c3 -eq 1) ("instances=" + $c3)

# --- 4. a killed owner must not leave a stale lock ------------------------
Write-Host ""
Write-Host "-- 4. crash recovery --"
$victim = Get-Process -Name "MKYBOOT Launcher" -ErrorAction SilentlyContinue | Select-Object -First 1
if ($victim -ne $null) {
    $victim.Kill()
    $null = $victim.WaitForExit(8000)
    Start-Sleep -Seconds 2
    $c4 = Count-Inst
    Report "killed instance is gone" ($c4 -eq 0) ("instances=" + $c4)

    Start-Process -FilePath $Exe | Out-Null
    Start-Sleep -Seconds 3
    $c5 = Count-Inst
    Report "relaunch after crash succeeds (no stale lock)" ($c5 -eq 1) ("instances=" + $c5)
} else {
    Report "a victim instance was present to kill" $false "no running instance"
}

# --- 5. graceful exit frees the lock --------------------------------------
Write-Host ""
Write-Host "-- 5. clean shutdown frees the lock --"
Clear-Inst
Start-Process -FilePath $Exe | Out-Null
Start-Sleep -Seconds 3
$c6 = Count-Inst
Report "instance running before shutdown" ($c6 -eq 1) ("instances=" + $c6)
Clear-Inst
Start-Process -FilePath $Exe | Out-Null
Start-Sleep -Seconds 3
$c7 = Count-Inst
Report "relaunch after clean shutdown succeeds" ($c7 -eq 1) ("instances=" + $c7)

Clear-Inst
Report "no launcher left running after the test" ((Count-Inst) -eq 0) ("instances=" + (Count-Inst))

Write-Host ""
if ($script:Failures -eq 0) {
    Write-Host "RESULT: PASS (all checks)"
    exit 0
} else {
    Write-Host ("RESULT: FAIL (" + $script:Failures + " failed)")
    exit 1
}