# Shared Win32 interop + helpers for the launcher UI test scripts
# (uitest.ps1; new scenarios can dot-source the same helpers).
#   . "$PSScriptRoot\uitest_lib.ps1"
# Callers may set $script:Shot before calling New-Snap.

Add-Type -TypeDefinition @'
using System;
using System.Text;
using System.Runtime.InteropServices;
public class WT {
  public struct RECT { public int Left; public int Top; public int Right; public int Bottom; }
  [StructLayout(LayoutKind.Sequential)]
  public struct GTI {
    public int cbSize; public int flags; public IntPtr hwndActive; public IntPtr hwndFocus;
    public IntPtr hwndCapture; public IntPtr hwndMenuOwner; public IntPtr hwndMoveSize;
    public IntPtr hwndCaret; public RECT rcCaret;
  }
  // Optional string parameters are declared as IntPtr so NULL can be passed
  // explicitly (PowerShell marshals $null as an empty string otherwise).
  [DllImport("user32.dll", CharSet=CharSet.Unicode, EntryPoint="FindWindowW")]
  public static extern IntPtr FindWindow(string cls, IntPtr title);
  [DllImport("user32.dll", CharSet=CharSet.Unicode, EntryPoint="FindWindowExW")]
  public static extern IntPtr FindChild(IntPtr parent, IntPtr after, IntPtr cls, IntPtr title);
  [DllImport("user32.dll")] public static extern bool PostMessageW(IntPtr h, uint m, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] public static extern bool IsWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll")] public static extern bool IsWindowEnabled(IntPtr h);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int cmd);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll")] public static extern bool GetGUIThreadInfo(uint tid, ref GTI g);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowTextW(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetClassNameW(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll")] public static extern IntPtr SendMessageW(IntPtr h, uint m, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] public static extern void mouse_event(uint f, uint dx, uint dy, uint d, IntPtr e);
  public static string Text(IntPtr h) { var sb = new StringBuilder(256); GetWindowTextW(h, sb, 256); return sb.ToString(); }
  public static string Cls(IntPtr h) { var sb = new StringBuilder(256); GetClassNameW(h, sb, 256); return sb.ToString(); }
  public static IntPtr Focus(uint tid) { var g = new GTI(); g.cbSize = Marshal.SizeOf(typeof(GTI)); GetGUIThreadInfo(tid, ref g); return g.hwndFocus; }
  public static void Click(int x, int y) {
    SetCursorPos(x, y);
    System.Threading.Thread.Sleep(150);
    mouse_event(0x0002, 0, 0, 0, IntPtr.Zero);
    mouse_event(0x0004, 0, 0, 0, IntPtr.Zero);
  }
}
'@ -Language CSharp
Add-Type -AssemblyName System.Drawing

$script:Shot = "$env:TEMP\mky_ui"
$script:pass = 0
$script:fail = 0
$script:RunKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$script:RunVal = 'MKYBOOT Launcher'

function Report([string]$check, [bool]$ok, [string]$detail) {
  if ($ok) { $script:pass++ } else { $script:fail++ }
  $tag = if ($ok) { 'PASS' } else { 'FAIL' }
  Write-Output ("{0} | {1,-38} | {2}" -f $tag, $check, $detail)
}
function New-Snap([string]$name, [IntPtr]$h) {
  $r = New-Object WT+RECT
  [void][WT]::GetWindowRect($h, [ref]$r)
  $bmp = New-Object System.Drawing.Bitmap (($r.Right - $r.Left)), (($r.Bottom - $r.Top))
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.CopyFromScreen($r.Left, $r.Top, 0, 0, $bmp.Size)
  $path = "$script:Shot`_$name.png"
  $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
  $g.Dispose(); $bmp.Dispose()
  return $path
}
function Get-Children([IntPtr]$parent) {
  $list = @()
  $c = [WT]::FindChild($parent, [IntPtr]::Zero, [IntPtr]::Zero, [IntPtr]::Zero)
  while ($c -ne [IntPtr]::Zero) {
    $r = New-Object WT+RECT
    [void][WT]::GetWindowRect($c, [ref]$r)
    $list += [pscustomobject]@{ H = $c; Cls = [WT]::Cls($c); Txt = [WT]::Text($c); R = $r
      Cx = [int](($r.Left + $r.Right) / 2); Cy = [int](($r.Top + $r.Bottom) / 2) }
    $c = [WT]::FindChild($parent, $c, [IntPtr]::Zero, [IntPtr]::Zero)
  }
  return $list
}
function Wait-Window([string]$cls, [int]$ms) {
  $h = [IntPtr]::Zero
  for ($i = 0; $i -lt [int]($ms / 200) -and $h -eq [IntPtr]::Zero; $i++) {
    Start-Sleep -Milliseconds 200
    $h = [WT]::FindWindow($cls, [IntPtr]::Zero)
  }
  return $h
}
function Get-RunValue {
  return (Get-ItemProperty -Path $script:RunKey -Name $script:RunVal -ErrorAction SilentlyContinue).$($script:RunVal)
}
# Ownerdraw rows keep no Win32 check state (Windows ignores BM_SETCHECK and
# BM_GETCHECK for BS_OWNERDRAW), so the drawn tick and the persisted setting
# are the evidence: Count-Color counts pixels matching an RGB value (+/- tol)
# inside a snapshot rectangle, Get-StartupJson reads the saved setting.
function Count-Color([string]$png, [int]$x, [int]$y, [int]$w, [int]$h,
                     [int]$r, [int]$g, [int]$b, [int]$tol = 12) {
  $img = [System.Drawing.Image]::FromFile($png)
  $n = 0
  for ($yy = $y; $yy -lt ($y + $h); $yy++) {
    for ($xx = $x; $xx -lt ($x + $w); $xx++) {
      $c = $img.GetPixel($xx, $yy)
      if ([Math]::Abs($c.R - $r) -le $tol -and
          [Math]::Abs($c.G - $g) -le $tol -and
          [Math]::Abs($c.B - $b) -le $tol) { $n++ }
    }
  }
  $img.Dispose()
  return $n
}
function Get-StartupJson {
  $p = Join-Path $env:APPDATA 'MKYBOOT\launcher.json'
  if (-not (Test-Path $p)) { return $null }
  try { return (Get-Content $p -Raw | ConvertFrom-Json).start_with_windows }
  catch { return $null }
}
function Start-Launcher([string]$exe) {
  Get-Process -Name 'MKYBOOT Launcher' -ErrorAction SilentlyContinue | Stop-Process -Force
  Start-Sleep -Milliseconds 400
  return Start-Process -FilePath $exe -PassThru
}
