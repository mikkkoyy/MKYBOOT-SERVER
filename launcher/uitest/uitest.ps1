param(
  [string]$Exe  = '',
  [string]$Shot = "$env:TEMP\mky_ui"
)

# Runtime UI harness: drives the built launcher the way a user would (focus,
# hover, clicks, modal dialog, checkbox round trip, minimize, tray) and prints
# PASS/FAIL per behaviour. Screenshots land in "<Shot>_<name>.png" for review.
#
#   make build                                  # in launcher/
#   powershell -ExecutionPolicy Bypass -File .\uitest\uitest.ps1
#
# The launcher must run on a desktop session (it inspects real pixels and
# posts real mouse/keyboard input); run it with no other windows in the way.
if (-not $Exe) { $Exe = Join-Path $PSScriptRoot '..\dist\MKYBOOT Launcher.exe' }
if (-not (Test-Path $Exe)) { throw "launcher executable not found: $Exe (run 'make build' in launcher/ first)" }
$Exe = (Resolve-Path $Exe).Path

. "$PSScriptRoot\uitest_lib.ps1"
$script:Shot = $Shot

$proc = Start-Launcher $Exe
$main = Wait-Window 'MkybootLauncherWnd' 12000
Report 'window created' ($main -ne [IntPtr]::Zero) `
  ("hwnd=$main pid=$($proc.Id) title='`"$([WT]::Text($main))`"")
if ($main -eq [IntPtr]::Zero) { exit 1 }

[void][WT]::SetForegroundWindow($main)
Start-Sleep -Milliseconds 600
$wr = New-Object WT+RECT
[void][WT]::GetWindowRect($main, [ref]$wr)
Report 'window size 456x469' (($wr.Right - $wr.Left) -eq 456 -and ($wr.Bottom - $wr.Top) -eq 469) `
  ("outer {0}x{1} at {2},{3}" -f ($wr.Right - $wr.Left), ($wr.Bottom - $wr.Top), $wr.Left, $wr.Top)

$pid0 = 0
$tid = [WT]::GetWindowThreadProcessId($main, [ref]$pid0)

# ---- child controls -----------------------------------------------------
$kids = Get-Children $main
$texts = @($kids | ForEach-Object { $_.Txt } | Where-Object { $_ -ne '' } | Sort-Object)
$want = @('Open Dashboard', 'Restart Server', 'Server Status', 'Start with Windows',
          'Stop Server', 'http://127.0.0.1:8888') | Sort-Object
Report 'control labels' (($texts -join '|') -eq ($want -join '|')) ($texts -join '|')
Report 'six child controls' ($kids.Count -eq 6) ("count={0}" -f $kids.Count)
Report 'address field is an EDIT' (@($kids | Where-Object { $_.Cls -eq 'EDIT' }).Count -eq 1) `
  (($kids | Where-Object { $_.Cls -eq 'EDIT' } | ForEach-Object { $_.Cls }) -join ',')

# ---- keyboard navigation (Tab) ------------------------------------------
$f0 = [WT]::Focus($tid)
[void][WT]::PostMessageW($main, 0x0100, [IntPtr]9, [IntPtr]0)   # WM_KEYDOWN VK_TAB
[void][WT]::PostMessageW($main, 0x0101, [IntPtr]9, [IntPtr]0)   # WM_KEYUP VK_TAB
Start-Sleep -Milliseconds 600
$f1 = [WT]::Focus($tid)
Report 'Tab moves focus' ($f1 -ne $f0) `
  ("before='{0}' -> after='{1}'" -f [WT]::Text($f0), [WT]::Text($f1))

# ---- hover highlight ----------------------------------------------------
$status = $kids | Where-Object { $_.Txt -eq 'Server Status' } | Select-Object -First 1
$chk = $kids | Where-Object { $_.Txt -eq 'Start with Windows' } | Select-Object -First 1
# Sample a band inside the button face (clear of the rounded corners).
$bx = ($status.R.Left - $wr.Left) + 24
$by = ($status.R.Top - $wr.Top) + 14
$bw = ($status.R.Right - $status.R.Left) - 48
$bh = ($status.R.Bottom - $status.R.Top) - 28
# The same band over the neighbouring row is used as the "not highlighted" probe.
$ox = ($chk.R.Left - $wr.Left) + 24
$oy = ($chk.R.Top - $wr.Top) + 4
$ow = ($chk.R.Right - $chk.R.Left) - 48
$oh = ($chk.R.Bottom - $chk.R.Top) - 8
[void][WT]::SetCursorPos($wr.Left + 4, $wr.Top + 8)  # over the title bar: nothing hovered
Start-Sleep -Milliseconds 800
$snapIdle = New-Snap 'idle' $main
$hotIdle = Count-Color $snapIdle $bx $by $bw $bh 47 57 77   # colBtnHot
# Move onto the button without clicking it: only the hovered row highlights.
[void][WT]::SetCursorPos($status.Cx - 40, $status.Cy)
Start-Sleep -Milliseconds 150
[void][WT]::SetCursorPos($status.Cx, $status.Cy)
Start-Sleep -Milliseconds 800
$snapHover = New-Snap 'hovered' $main
$hotNow = Count-Color $snapHover $bx $by $bw $bh 47 57 77
$hotOther = Count-Color $snapHover $ox $oy $ow $oh 47 57 77
Report 'hover highlights only the control under the pointer' `
  ($hotIdle -eq 0 -and $hotNow -gt 0 -and $hotOther -eq 0) `
  ("hot px: idle=$hotIdle hovered=$hotNow neighbouring=$hotOther")
Write-Output "  hover snapshots: $snapIdle / $snapHover"

# ---- offline server actions --------------------------------------------
$restart = $kids | Where-Object { $_.Txt -eq 'Restart Server' } | Select-Object -First 1
$stop    = $kids | Where-Object { $_.Txt -eq 'Stop Server' } | Select-Object -First 1
Report 'Restart disabled while offline' (-not [WT]::IsWindowEnabled($restart.H)) `
  ("IsWindowEnabled={0}" -f [WT]::IsWindowEnabled($restart.H))
Report 'Stop disabled while offline' (-not [WT]::IsWindowEnabled($stop.H)) `
  ("IsWindowEnabled={0}" -f [WT]::IsWindowEnabled($stop.H))
Report 'Dashboard enabled while offline' ([WT]::IsWindowEnabled($status.H)) `
  ("IsWindowEnabled={0}" -f [WT]::IsWindowEnabled($status.H))

# The disabled button cannot be clicked, so drive the same control id the
# button would send (WM_COMMAND with idBtnRestart) to reach the offline
# control path: it must raise the modal "not reachable" notice, disable the
# owner window, and close again through OK.
[void][WT]::PostMessageW($main, 0x0111, [IntPtr]1004, $restart.H)
$dlg = Wait-Window 'MkybootDialogWnd' 6000
Report 'offline control opens notice dialog' ($dlg -ne [IntPtr]::Zero) `
  ("hwnd=$dlg title='`"$([WT]::Text($dlg))`"'")
if ($dlg -ne [IntPtr]::Zero) {
  Start-Sleep -Milliseconds 400
  Report 'main window disabled (modal)' (-not [WT]::IsWindowEnabled($main)) `
    ("IsWindowEnabled={0}" -f [WT]::IsWindowEnabled($main))
  Write-Output "  dialog snapshot: $(New-Snap 'notice' $dlg)"
  $okBtn = (Get-Children $dlg) | Where-Object { $_.Txt -eq 'OK' } | Select-Object -First 1
  if ($okBtn) {
    [WT]::Click($okBtn.Cx, $okBtn.Cy)
    Start-Sleep -Milliseconds 900
    Report 'OK closes the dialog' (-not [WT]::IsWindow($dlg)) `
      ("IsWindow={0}" -f [WT]::IsWindow($dlg))
    Report 'main window re-enabled' ([WT]::IsWindowEnabled($main)) `
      ("IsWindowEnabled={0}" -f [WT]::IsWindowEnabled($main))
  } else {
    Report 'OK button found' $false 'no child button labelled OK'
  }
}

# ---- "Start with Windows" toggle (round trip, state left as found) ------
# Ownerdraw rows cannot store a check state (Windows ignores BM_SETCHECK and
# BM_GETCHECK for BS_OWNERDRAW), so the launcher tracks it and the evidence is
# the drawn accent tick plus the persisted state (config JSON + Run value).
$tkx = $chk.R.Left - $wr.Left
$tky = $chk.R.Top - $wr.Top
$tkw = $chk.R.Right - $chk.R.Left
$tkh = $chk.R.Bottom - $chk.R.Top
function Tick([string]$png) { Count-Color $png $tkx $tky $tkw $tkh 59 130 246 }  # colAccent
$snapBefore = New-Snap 'checkbox_before' $main
$tick0 = Tick $snapBefore
$run0 = [bool](Get-RunValue)
$cfg0 = [bool](Get-StartupJson)
Report 'startup row mirrors the saved setting' ((($tick0 -gt 0) -eq $run0) -and ($run0 -eq $cfg0)) `
  ("tick px={0} Run={1} config={2} ({3})" -f $tick0, $run0, $cfg0, $snapBefore)

[WT]::Click($chk.Cx, $chk.Cy)
Start-Sleep -Milliseconds 1200
$snapOn = New-Snap 'checkbox_toggled' $main
$tick1 = Tick $snapOn
$run1 = [bool](Get-RunValue)
$cfg1 = [bool](Get-StartupJson)
Report 'click flips the tick, the config and the Run value' `
  (((($tick1 -gt 0) -ne ($tick0 -gt 0))) -and ($run1 -eq (-not $run0)) -and ($cfg1 -eq (-not $cfg0))) `
  ("tick {0}->{1} Run {2}->{3} config {4}->{5} ({6})" -f ($tick0 -gt 0), ($tick1 -gt 0), `
   $run0, $run1, $cfg0, $cfg1, $snapOn)

[WT]::Click($chk.Cx, $chk.Cy)
Start-Sleep -Milliseconds 1200
$snapBack = New-Snap 'checkbox_restored' $main
$tick2 = Tick $snapBack
$run2 = [bool](Get-RunValue)
$cfg2 = [bool](Get-StartupJson)
Report 'second click restores the original state' `
  (((($tick2 -gt 0) -eq ($tick0 -gt 0))) -and ($run2 -eq $run0) -and ($cfg2 -eq $cfg0)) `
  ("tick {0}->{1} Run {2}->{3} config {4}->{5} ({6})" -f ($tick1 -gt 0), ($tick2 -gt 0), `
   $run1, $run2, $cfg1, $cfg2, $snapBack)

# ---- minimize to tray / restore ----------------------------------------
[void][WT]::ShowWindow($main, 6)  # SW_MINIMIZE
Start-Sleep -Milliseconds 1200
Report 'minimize hides window to tray' (-not [WT]::IsWindowVisible($main)) `
  ("IsWindowVisible={0}" -f [WT]::IsWindowVisible($main))
$trayWin = [WT]::FindWindow('Shell_TrayWnd', [IntPtr]::Zero)
if ($trayWin -ne [IntPtr]::Zero) { Write-Output "  tray snapshot: $(New-Snap 'tray' $trayWin)" }

[void][WT]::PostMessageW($main, 0x8004, [IntPtr]1, [IntPtr]0x0202)  # tray left-click -> restore
Start-Sleep -Milliseconds 1200
Report 'tray left-click restores window' ([WT]::IsWindowVisible($main)) `
  ("IsWindowVisible={0}" -f [WT]::IsWindowVisible($main))
Write-Output "  restored snapshot: $(New-Snap 'restored' $main)"

# ---- tray context menu -------------------------------------------------
[void][WT]::SetCursorPos([int](($wr.Left + $wr.Right) / 2), [int](($wr.Top + $wr.Bottom) / 2))
Start-Sleep -Milliseconds 200
[void][WT]::PostMessageW($main, 0x8004, [IntPtr]1, [IntPtr]0x0205)  # tray right-click -> popup menu
Start-Sleep -Milliseconds 1200
Add-Type -AssemblyName System.Windows.Forms
$scr = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
$fs = New-Object System.Drawing.Bitmap $scr.Width, $scr.Height
$g = [System.Drawing.Graphics]::FromImage($fs)
$g.CopyFromScreen(0, 0, 0, 0, $fs.Size)
$menuShot = "$Shot`_traymenu.png"
$fs.Save($menuShot, [System.Drawing.Imaging.ImageFormat]::Png)
$g.Dispose(); $fs.Dispose()
Write-Output "  tray menu snapshot: $menuShot"
[WT]::Click(4, 4)   # clicking outside dismisses the popup
Start-Sleep -Milliseconds 800
Report 'alive after tray menu' (-not $proc.HasExited) ("HasExited={0}" -f $proc.HasExited)

# ---- exit via the tray Exit command ------------------------------------
[void][WT]::PostMessageW($main, 0x0111, [IntPtr]1008, [IntPtr]0)  # WM_COMMAND idTrayExit
for ($i = 0; $i -lt 25 -and -not $proc.HasExited; $i++) { Start-Sleep -Milliseconds 200 }
Report 'Exit terminates the process' $proc.HasExited ("HasExited={0}" -f $proc.HasExited)

Write-Output ""
Write-Output ("RESULT: {0} passed, {1} failed" -f $script:pass, $script:fail)
if ($script:fail -gt 0) { exit 1 }
