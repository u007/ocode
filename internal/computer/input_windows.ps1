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
        default {
            throw "unknown op '$Op'"
        }
    }
}
catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 1
}
