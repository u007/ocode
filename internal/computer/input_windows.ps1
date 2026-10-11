# ocode computer-use input helper.
#
# Ops (argv after -File <script>):
#   screen                      -> prints "<width> <height>"
#   screenshot <path>           -> writes PNG to <path>, prints "<width> <height>"
#   cursor                      -> prints "<x> <y>"
#   move <x> <y>
#   click <x> <y> <button> <count>   button: left|right|middle
#   drag <x1> <y1> <x2> <y2>
#   scroll <x> <y> <dir> <amount>    dir: up|down|left|right
#   type <base64-utf8-text>
#   key <vk> [<vk> ...]              modifiers first, main key last
#   windows                          -> one line per top-level window:
#                                       hwnd, app, title, x, y, w, h, minimized, focused (tab-separated)
#   window-focus <hwnd>
#   window-bounds <hwnd> <x> <y> <w> <h>
#   window-state <hwnd> <minimize|restore|maximize|close>
#
# Every op prints only its documented output. Any failure writes a message to
# stderr and exits 1.

param(
    [Parameter(Position = 0)]
    [string]$Op,
    [Parameter(Position = 1, ValueFromRemainingArguments = $true)]
    [string[]]$Rest
)

$ErrorActionPreference = 'Stop'

Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;

public class OcodeInput {
    [DllImport("user32.dll")]
    public static extern bool SetProcessDPIAware();

    [DllImport("user32.dll", SetLastError = true)]
    public static extern bool SetCursorPos(int X, int Y);

    [DllImport("user32.dll", SetLastError = true)]
    public static extern bool GetCursorPos(out POINT lpPoint);

    [DllImport("user32.dll", SetLastError = true)]
    public static extern uint SendInput(uint nInputs, INPUT[] pInputs, int cbSize);

    [StructLayout(LayoutKind.Sequential)]
    public struct POINT { public int X; public int Y; }

    [StructLayout(LayoutKind.Sequential)]
    public struct MOUSEINPUT {
        public int dx;
        public int dy;
        public uint mouseData;
        public uint dwFlags;
        public uint time;
        public IntPtr dwExtraInfo;
    }

    [StructLayout(LayoutKind.Sequential)]
    public struct KEYBDINPUT {
        public ushort wVk;
        public ushort wScan;
        public uint dwFlags;
        public uint time;
        public IntPtr dwExtraInfo;
    }

    [StructLayout(LayoutKind.Sequential)]
    public struct HARDWAREINPUT {
        public uint uMsg;
        public ushort wParamL;
        public ushort wParamH;
    }

    // One INPUT type for mouse and keyboard events: SendInput takes an
    // INPUT[] and cbSize must be SizeOf(INPUT). The explicit-layout union
    // keeps the 8-byte dwExtraInfo aligned on 64-bit.
    [StructLayout(LayoutKind.Explicit)]
    public struct InputUnion {
        [FieldOffset(0)] public MOUSEINPUT mi;
        [FieldOffset(0)] public KEYBDINPUT ki;
        [FieldOffset(0)] public HARDWAREINPUT hi;
    }

    [StructLayout(LayoutKind.Sequential)]
    public struct INPUT {
        public uint type;
        public InputUnion u;
    }

    public const uint INPUT_MOUSE = 0;
    public const uint INPUT_KEYBOARD = 1;
    public const uint MOUSEEVENTF_LEFTDOWN = 0x0002;
    public const uint MOUSEEVENTF_LEFTUP = 0x0004;
    public const uint MOUSEEVENTF_RIGHTDOWN = 0x0008;
    public const uint MOUSEEVENTF_RIGHTUP = 0x0010;
    public const uint MOUSEEVENTF_MIDDLEDOWN = 0x0020;
    public const uint MOUSEEVENTF_MIDDLEUP = 0x0040;
    public const uint MOUSEEVENTF_WHEEL = 0x0800;
    public const uint MOUSEEVENTF_HWHEEL = 0x1000;
    public const uint KEYEVENTF_KEYUP = 0x0002;
    public const uint KEYEVENTF_UNICODE = 0x0004;

    private static void Send(INPUT[] inputs) {
        uint sent = SendInput((uint)inputs.Length, inputs, Marshal.SizeOf(typeof(INPUT)));
        if (sent != (uint)inputs.Length) {
            throw new Exception("SendInput sent " + sent + " of " + inputs.Length +
                " event(s), last error " + Marshal.GetLastWin32Error());
        }
    }

    private static INPUT MouseEvent(uint flags, int data) {
        INPUT i = new INPUT();
        i.type = INPUT_MOUSE;
        i.u.mi.dwFlags = flags;
        // mouseData carries a signed wheel delta in an unsigned field.
        i.u.mi.mouseData = unchecked((uint)data);
        return i;
    }

    private static INPUT KeyInput(ushort vk, ushort scan, uint flags) {
        INPUT i = new INPUT();
        i.type = INPUT_KEYBOARD;
        i.u.ki.wVk = vk;
        i.u.ki.wScan = scan;
        i.u.ki.dwFlags = flags;
        return i;
    }

    // MoveTo positions the cursor absolutely. Button and wheel events then
    // carry no relative-move flag, so they land where the cursor already is.
    public static void MoveTo(int x, int y) {
        if (!SetCursorPos(x, y)) {
            throw new Exception("SetCursorPos failed, last error " + Marshal.GetLastWin32Error());
        }
    }

    public static POINT Cursor() {
        POINT p;
        if (!GetCursorPos(out p)) {
            throw new Exception("GetCursorPos failed, last error " + Marshal.GetLastWin32Error());
        }
        return p;
    }

    public static void ButtonEvent(uint flags) {
        Send(new INPUT[] { MouseEvent(flags, 0) });
    }

    public static void Wheel(uint flags, int delta) {
        Send(new INPUT[] { MouseEvent(flags, delta) });
    }

    public static void KeyEvent(ushort vk, bool up) {
        Send(new INPUT[] { KeyInput(vk, 0, up ? KEYEVENTF_KEYUP : 0) });
    }

    public static void UnicodeUnit(ushort unit, bool up) {
        Send(new INPUT[] { KeyInput(0, unit, KEYEVENTF_UNICODE | (up ? KEYEVENTF_KEYUP : 0)) });
    }
}
"@

Add-Type -TypeDefinition @"
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;

public class OcodeWin {
    public delegate bool EnumProc(IntPtr hWnd, IntPtr lParam);

    [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc cb, IntPtr lParam);
    [DllImport("user32.dll")] static extern bool IsWindow(IntPtr h);
    [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
    [DllImport("user32.dll")] static extern bool IsIconic(IntPtr h);
    [DllImport("user32.dll")] static extern bool IsZoomed(IntPtr h);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] static extern int GetWindowText(IntPtr h, StringBuilder s, int max);
    [DllImport("user32.dll")] static extern int GetWindowTextLength(IntPtr h);
    [DllImport("user32.dll")] static extern bool GetWindowRect(IntPtr h, out RECT r);
    [DllImport("user32.dll")] static extern bool GetWindowPlacement(IntPtr h, ref WINDOWPLACEMENT p);
    [DllImport("user32.dll")] static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")] static extern bool SetForegroundWindow(IntPtr h);
    [DllImport("user32.dll")] static extern bool ShowWindow(IntPtr h, int cmd);
    [DllImport("user32.dll")] static extern bool SetWindowPos(IntPtr h, IntPtr after, int x, int y, int w, int cx, uint flags);
    [DllImport("user32.dll")] static extern bool PostMessage(IntPtr h, uint msg, IntPtr w, IntPtr l);
    [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
    [DllImport("user32.dll", EntryPoint = "GetWindowLongPtrW")] static extern IntPtr GetWindowLongPtr(IntPtr h, int idx);
    [DllImport("user32.dll")] static extern void keybd_event(byte vk, byte scan, uint flags, UIntPtr extra);
    [DllImport("dwmapi.dll")] static extern int DwmGetWindowAttribute(IntPtr h, int attr, out int value, int size);

    [StructLayout(LayoutKind.Sequential)]
    public struct RECT { public int Left, Top, Right, Bottom; }

    [StructLayout(LayoutKind.Sequential)]
    public struct POINT { public int X, Y; }

    [StructLayout(LayoutKind.Sequential)]
    public struct WINDOWPLACEMENT {
        public int length, flags, showCmd;
        public POINT ptMinPosition, ptMaxPosition;
        public RECT rcNormalPosition;
    }

    const int GWL_EXSTYLE = -20;
    const long WS_EX_TOOLWINDOW = 0x80;
    const int DWMWA_CLOAKED = 14;
    const int SW_MINIMIZE = 6, SW_MAXIMIZE = 3, SW_RESTORE = 9;
    const uint SWP_NOZORDER = 0x4, SWP_NOACTIVATE = 0x10;
    const uint WM_CLOSE = 0x10;

    public class Info {
        public long Hwnd; public uint Pid; public string Title;
        public int X, Y, W, H; public bool Minimized, Focused;
    }

    static IntPtr H(long v) {
        IntPtr h = new IntPtr(v);
        if (!IsWindow(h)) throw new Exception("window " + v + " no longer exists, list windows again");
        return h;
    }

    // Top-level windows a user would call a window: visible, titled, not a
    // tool window, not cloaked (other virtual desktop / suspended UWP shell).
    public static List<Info> List() {
        var res = new List<Info>();
        IntPtr fg = GetForegroundWindow();
        EnumWindows(delegate (IntPtr h, IntPtr l) {
            if (!IsWindowVisible(h)) return true;
            int len = GetWindowTextLength(h);
            if (len == 0) return true;
            if ((GetWindowLongPtr(h, GWL_EXSTYLE).ToInt64() & WS_EX_TOOLWINDOW) != 0) return true;
            int cloaked;
            if (DwmGetWindowAttribute(h, DWMWA_CLOAKED, out cloaked, 4) == 0 && cloaked != 0) return true;
            var sb = new StringBuilder(len + 1);
            GetWindowText(h, sb, sb.Capacity);
            var i = new Info();
            i.Hwnd = h.ToInt64();
            i.Title = sb.ToString();
            uint pid; GetWindowThreadProcessId(h, out pid); i.Pid = pid;
            i.Minimized = IsIconic(h);
            RECT r;
            if (i.Minimized) {
                // GetWindowRect reports -32000 for a minimized window; the
                // restored bounds live in the placement.
                var p = new WINDOWPLACEMENT(); p.length = Marshal.SizeOf(typeof(WINDOWPLACEMENT));
                GetWindowPlacement(h, ref p);
                r = p.rcNormalPosition;
            } else {
                GetWindowRect(h, out r);
            }
            i.X = r.Left; i.Y = r.Top; i.W = r.Right - r.Left; i.H = r.Bottom - r.Top;
            i.Focused = (h == fg);
            res.Add(i);
            return true;
        }, IntPtr.Zero);
        return res;
    }

    public static void Focus(long v) {
        IntPtr h = H(v);
        if (IsIconic(h)) ShowWindow(h, SW_RESTORE);
        // Windows refuses SetForegroundWindow from a process that did not
        // receive the last input; a synthetic Alt press lifts that lock.
        keybd_event(0x12, 0, 0, UIntPtr.Zero);
        keybd_event(0x12, 0, 2, UIntPtr.Zero);
        SetForegroundWindow(h);
        if (GetForegroundWindow() != h) throw new Exception("could not bring window " + v + " to the foreground");
    }

    public static void Bounds(long v, int x, int y, int w, int h2) {
        IntPtr h = H(v);
        if (IsIconic(h) || IsZoomed(h)) ShowWindow(h, SW_RESTORE);
        if (!SetWindowPos(h, IntPtr.Zero, x, y, w, h2, SWP_NOZORDER | SWP_NOACTIVATE))
            throw new Exception("SetWindowPos failed, last error " + Marshal.GetLastWin32Error());
    }

    public static void State(long v, string state) {
        IntPtr h = H(v);
        switch (state) {
            case "minimize": ShowWindow(h, SW_MINIMIZE); break;
            case "restore": ShowWindow(h, SW_RESTORE); break;
            case "maximize": ShowWindow(h, SW_MAXIMIZE); break;
            case "close":
                if (!PostMessage(h, WM_CLOSE, IntPtr.Zero, IntPtr.Zero))
                    throw new Exception("PostMessage WM_CLOSE failed, last error " + Marshal.GetLastWin32Error());
                break;
            default: throw new Exception("unknown window state '" + state + "'");
        }
    }
}
"@

# Must run before any screen bounds, capture or coordinate call so every op
# works in physical pixels. It returns false when the process is already
# DPI-aware through its manifest, which is not a failure, so the result is
# intentionally not treated as an error.
[OcodeInput]::SetProcessDPIAware() | Out-Null

function Confirm-ArgCount([int]$n) {
    if ($null -eq $Rest -or $Rest.Count -lt $n) {
        throw "op '$Op' requires $n argument(s)"
    }
}

function Get-ButtonFlags([string]$button) {
    switch ($button) {
        "left" { return @([OcodeInput]::MOUSEEVENTF_LEFTDOWN, [OcodeInput]::MOUSEEVENTF_LEFTUP) }
        "right" { return @([OcodeInput]::MOUSEEVENTF_RIGHTDOWN, [OcodeInput]::MOUSEEVENTF_RIGHTUP) }
        "middle" { return @([OcodeInput]::MOUSEEVENTF_MIDDLEDOWN, [OcodeInput]::MOUSEEVENTF_MIDDLEUP) }
        default { throw "unknown mouse button '$button'" }
    }
}

# One wheel notch is 120 units; up and right are positive.
function Get-WheelEvent([string]$dir, [int]$amount) {
    switch ($dir) {
        "up" { return @([OcodeInput]::MOUSEEVENTF_WHEEL, 120 * $amount) }
        "down" { return @([OcodeInput]::MOUSEEVENTF_WHEEL, -120 * $amount) }
        "right" { return @([OcodeInput]::MOUSEEVENTF_HWHEEL, 120 * $amount) }
        "left" { return @([OcodeInput]::MOUSEEVENTF_HWHEEL, -120 * $amount) }
        default { throw "unknown scroll direction '$dir'" }
    }
}

try {
    switch ($Op) {
        "screen" {
            $b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
            "$($b.Width) $($b.Height)"
            break
        }
        "screenshot" {
            Confirm-ArgCount 1
            $path = $Rest[0]
            $b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
            $bmp = New-Object System.Drawing.Bitmap($b.Width, $b.Height)
            $gfx = [System.Drawing.Graphics]::FromImage($bmp)
            try {
                $gfx.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
                $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
            }
            finally {
                $gfx.Dispose()
                $bmp.Dispose()
            }
            "$($b.Width) $($b.Height)"
            break
        }
        "cursor" {
            $p = [OcodeInput]::Cursor()
            "$($p.X) $($p.Y)"
            break
        }
        "move" {
            Confirm-ArgCount 2
            [OcodeInput]::MoveTo([int]$Rest[0], [int]$Rest[1])
            break
        }
        "click" {
            Confirm-ArgCount 4
            $x = [int]$Rest[0]
            $y = [int]$Rest[1]
            $flags = Get-ButtonFlags $Rest[2]
            $count = [int]$Rest[3]
            if ($count -lt 1) { throw "click count must be at least 1, got $count" }
            [OcodeInput]::MoveTo($x, $y)
            # No sleep between pairs: the OS needs them inside the
            # double-click interval to report a double click.
            for ($i = 0; $i -lt $count; $i++) {
                [OcodeInput]::ButtonEvent($flags[0])
                [OcodeInput]::ButtonEvent($flags[1])
            }
            break
        }
        "drag" {
            Confirm-ArgCount 4
            $x1 = [int]$Rest[0]
            $y1 = [int]$Rest[1]
            $x2 = [int]$Rest[2]
            $y2 = [int]$Rest[3]
            [OcodeInput]::MoveTo($x1, $y1)
            [OcodeInput]::ButtonEvent([OcodeInput]::MOUSEEVENTF_LEFTDOWN)
            Start-Sleep -Milliseconds 20
            # Intermediate moves: many controls do not start a drag until the
            # cursor moves while the button is held.
            for ($step = 1; $step -le 3; $step++) {
                $ix = $x1 + [int](($x2 - $x1) * $step / 4)
                $iy = $y1 + [int](($y2 - $y1) * $step / 4)
                [OcodeInput]::MoveTo($ix, $iy)
                Start-Sleep -Milliseconds 20
            }
            [OcodeInput]::MoveTo($x2, $y2)
            Start-Sleep -Milliseconds 20
            [OcodeInput]::ButtonEvent([OcodeInput]::MOUSEEVENTF_LEFTUP)
            break
        }
        "scroll" {
            Confirm-ArgCount 4
            $x = [int]$Rest[0]
            $y = [int]$Rest[1]
            $amount = [int]$Rest[3]
            if ($amount -lt 1) { throw "scroll amount must be at least 1, got $amount" }
            $wheel = Get-WheelEvent $Rest[2] $amount
            [OcodeInput]::MoveTo($x, $y)
            [OcodeInput]::Wheel($wheel[0], $wheel[1])
            break
        }
        "type" {
            Confirm-ArgCount 1
            $text = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($Rest[0]))
            # ToCharArray yields UTF-16 code units, so surrogate pairs are sent
            # as the two units KEYEVENTF_UNICODE expects.
            foreach ($ch in $text.ToCharArray()) {
                $unit = [ushort]$ch
                [OcodeInput]::UnicodeUnit($unit, $false)
                [OcodeInput]::UnicodeUnit($unit, $true)
            }
            break
        }
        "key" {
            Confirm-ArgCount 1
            $codes = @()
            foreach ($a in $Rest) { $codes += [ushort]$a }
            $main = $codes.Count - 1
            for ($i = 0; $i -lt $main; $i++) { [OcodeInput]::KeyEvent($codes[$i], $false) }
            [OcodeInput]::KeyEvent($codes[$main], $false)
            [OcodeInput]::KeyEvent($codes[$main], $true)
            for ($i = $main - 1; $i -ge 0; $i--) { [OcodeInput]::KeyEvent($codes[$i], $true) }
            break
        }
        "windows" {
            foreach ($w in [OcodeWin]::List()) {
                # The process can exit between enumeration and lookup; a blank app
                # name is the correct report for a window that is already gone.
                $name = ""
                $proc = Get-Process -Id $w.Pid -ErrorAction SilentlyContinue
                if ($null -ne $proc) { $name = $proc.ProcessName }
                $title = $w.Title -replace "[\t\r\n]", " "
                $min = if ($w.Minimized) { 1 } else { 0 }
                $foc = if ($w.Focused) { 1 } else { 0 }
                "$($w.Hwnd)`t$name`t$title`t$($w.X)`t$($w.Y)`t$($w.W)`t$($w.H)`t$min`t$foc"
            }
            break
        }
        "window-focus" {
            Confirm-ArgCount 1
            [OcodeWin]::Focus([long]$Rest[0])
            break
        }
        "window-bounds" {
            Confirm-ArgCount 5
            [OcodeWin]::Bounds([long]$Rest[0], [int]$Rest[1], [int]$Rest[2], [int]$Rest[3], [int]$Rest[4])
            break
        }
        "window-state" {
            Confirm-ArgCount 2
            [OcodeWin]::State([long]$Rest[0], [string]$Rest[1])
            break
        }
        default {
            throw "unknown op '$Op'"
        }
    }
}
catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 1
}
