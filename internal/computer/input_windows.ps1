Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
using System.Text;

public class OcodeInput {
    [DllImport("user32.dll")]
    public static extern bool SetProcessDPIAware();

    [DllImport("user32.dll")]
    public static extern bool SetCursorPos(int x, int y);

    [DllImport("user32.dll")]
    public static extern bool GetCursorPos(out POINT lpPoint);

    [DllImport("user32.dll")]
    public static extern uint SendInput(uint nInputs, INPUT[] pInputs, int cbSize);

    [StructLayout(LayoutKind.Sequential)]
    public struct POINT { public int X; public int Y; }

    [StructLayout(LayoutKind.Sequential)]
    public struct MOUSEINPUT {
        public int dx, dy;
        public uint mouseData, dwFlags, time;
        public IntPtr dwExtraInfo;
    }

    [StructLayout(LayoutKind.Sequential)]
    public struct KEYBDINPUT {
        public ushort wVk, wScan;
        public uint dwFlags, time;
        public IntPtr dwExtraInfo;
    }

    [StructLayout(LayoutKind.Sequential)]
    public struct INPUT {
        public uint type;
        public InputUnion u;
    }

    [StructLayout(LayoutKind.Explicit)]
    public struct InputUnion {
        [FieldOffset(0)] public MOUSEINPUT mi;
        [FieldOffset(0)] public KEYBDINPUT ki;
    }

    public const uint INPUT_MOUSE = 0;
    public const uint INPUT_KEYBOARD = 1;
    public const uint MOUSEEVENTF_MOVE = 0x0001;
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
}
"@

param([string]$op, [string[]]$args)

switch ($op) {
    "screen" {
        $b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
        "$($b.Width) $($b.Height)"
    }
    "screenshot" {
        $path = $args[0]
        $b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
        $bmp = New-Object System.Drawing.Bitmap($b.Width, $b.Height)
        $gfx = [System.Drawing.Graphics]::FromImage($bmp)
        $gfx.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
        $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
        $gfx.Dispose()
        $bmp.Dispose()
    }
    "cursor" {
        $p = New-Object OcodeInput+POINT
        [OcodeInput]::GetCursorPos([ref]$p) | Out-Null
        "$($p.X) $($p.Y)"
    }
    "move" {
        [OcodeInput]::SetCursorPos([int]$args[0], [int]$args[1]) | Out-Null
    }
    "click" {
        $x = [int]$args[0]; $y = [int]$args[1]
        $button = $args[2]; $count = [int]$args[3]
        $downFlag = switch ($button) {
            "right" { [OcodeInput]::MOUSEEVENTF_RIGHTDOWN }
            "middle" { [OcodeInput]::MOUSEEVENTF_MIDDLEDOWN }
            default { [OcodeInput]::MOUSEEVENTF_LEFTDOWN }
        }
        $upFlag = switch ($button) {
            "right" { [OcodeInput]::MOUSEEVENTF_RIGHTUP }
            "middle" { [OcodeInput]::MOUSEEVENTF_MIDDLEUP }
            default { [OcodeInput]::MOUSEEVENTF_LEFTUP }
        }
        for ($i = 0; $i -lt $count; $i++) {
            $mi = New-Object OcodeInput+INPUT
            $mi.type = [OcodeInput]::INPUT_MOUSE
            $mi.u.mi.dx = $x; $mi.u.mi.dy = $y
            $mi.u.mi.dwFlags = $downFlag -bor [OcodeInput]::MOUSEEVENTF_MOVE
            [OcodeInput]::SendInput(1, [OcodeInput+INPUT[]]@($mi), [System.Runtime.InteropServices.Marshal]::SizeOf([OcodeInput+INPUT])) | Out-Null
            $mu = New-Object OcodeInput+INPUT
            $mu.type = [OcodeInput]::INPUT_MOUSE
            $mu.u.mi.dwFlags = $upFlag -bor [OcodeInput]::MOUSEEVENTF_MOVE
            [OcodeInput]::SendInput(1, [OcodeInput+INPUT[]]@($mu), [System.Runtime.InteropServices.Marshal]::SizeOf([OcodeInput+INPUT])) | Out-Null
        }
    }
    "drag" {
        $x1 = [int]$args[0]; $y1 = [int]$args[1]
        $x2 = [int]$args[2]; $y2 = [int]$args[3]
        $mi = New-Object OcodeInput+INPUT
        $mi.type = [OcodeInput]::INPUT_MOUSE
        $mi.u.mi.dx = $x1; $mi.u.mi.dy = $y1
        $mi.u.mi.dwFlags = [OcodeInput]::MOUSEEVENTF_LEFTDOWN -bor [OcodeInput]::MOUSEEVENTF_MOVE
        [OcodeInput]::SendInput(1, [OcodeInput+INPUT[]]@($mi), [System.Runtime.InteropServices.Marshal]::SizeOf([OcodeInput+INPUT])) | Out-Null
        $mm = New-Object OcodeInput+INPUT
        $mm.type = [OcodeInput]::INPUT_MOUSE
        $mm.u.mi.dx = $x2; $mm.u.mi.dy = $y2
        $mm.u.mi.dwFlags = [OcodeInput]::MOUSEEVENTF_LEFTDOWN -bor [OcodeInput]::MOUSEEVENTF_MOVE
        [OcodeInput]::SendInput(1, [OcodeInput+INPUT[]]@($mm), [System.Runtime.InteropServices.Marshal]::SizeOf([OcodeInput+INPUT])) | Out-Null
        $mu = New-Object OcodeInput+INPUT
        $mu.type = [OcodeInput]::INPUT_MOUSE
        $mu.u.mi.dwFlags = [OcodeInput]::MOUSEEVENTF_LEFTUP -bor [OcodeInput]::MOUSEEVENTF_MOVE
        [OcodeInput]::SendInput(1, [OcodeInput+INPUT[]]@($mu), [System.Runtime.InteropServices.Marshal]::SizeOf([OcodeInput+INPUT])) | Out-Null
    }
    "scroll" {
        $x = [int]$args[0]; $y = [int]$args[1]
        $dir = $args[2]; $amount = [int]$args[3]
        $mi = New-Object OcodeInput+INPUT
        $mi.type = [OcodeInput]::INPUT_MOUSE
        $mi.u.mi.dx = $x; $mi.u.mi.dy = $y
        $mi.u.mi.dwFlags = [OcodeInput]::MOUSEEVENTF_MOVE
        [OcodeInput]::SendInput(1, [OcodeInput+INPUT[]]@($mi), [System.Runtime.InteropServices.Marshal]::SizeOf([OcodeInput+INPUT])) | Out-Null
        $wi = New-Object OcodeInput+INPUT
        $wi.type = [OcodeInput]::INPUT_KEYBOARD
        $wi.u.ki.wVk = 0x0E
        $wi.u.ki.dwFlags = if ($dir -eq "horizontal") { 0x1 } else { 0 }
        [OcodeInput]::SendInput(1, [OcodeInput+INPUT[]]@($wi), [System.Runtime.InteropServices.Marshal]::SizeOf([OcodeInput+INPUT])) | Out-Null
    }
    "type" {
        $text = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($args[0]))
        foreach ($ch in $text) {
            $b = [System.BitConverter]::GetBytes([ushort]$ch)
            $ki = New-Object OcodeInput+KEYBDINPUT
            $ki.wVk = 0
            $ki.wScan = [System.BitConverter]::ToUInt16($b, 0)
            $ki.dwFlags = [OcodeInput]::KEYEVENTF_UNICODE
            [OcodeInput]::SendInput(1, [OcodeInput+KEYBDINPUT[]]@($ki), [System.Runtime.InteropServices.Marshal]::SizeOf([OcodeInput+KEYBDINPUT])) | Out-Null
            $ki2 = New-Object OcodeInput+KEYBDINPUT
            $ki2.wVk = 0
            $ki2.wScan = [System.BitConverter]::ToUInt16($b, 0)
            $ki2.dwFlags = [OcodeInput]::KEYEVENTF_UNICODE -bor [OcodeInput]::KEYEVENTF_KEYUP
            [OcodeInput]::SendInput(1, [OcodeInput+KEYBDINPUT[]]@($ki2), [System.Runtime.InteropServices.Marshal]::SizeOf([OcodeInput+KEYBDINPUT])) | Out-Null
        }
    }
    "key" {
        foreach ($arg in $args) {
            $code = [int]$arg
            $ki = New-Object OcodeInput+KEYBDINPUT
            $ki.wVk = [ushort]$code
            $ki.dwFlags = 0
            [OcodeInput]::SendInput(1, [OcodeInput+KEYBDINPUT[]]@($ki), [System.Runtime.InteropServices.Marshal]::SizeOf([OcodeInput+KEYBDINPUT])) | Out-Null
        }
        for ($i = $args.Length - 1; $i -ge 0; $i--) {
            $code = [int]$args[$i]
            $ki = New-Object OcodeInput+KEYBDINPUT
            $ki.wVk = [ushort]$code
            $ki.dwFlags = [OcodeInput]::KEYEVENTF_KEYUP
            [OcodeInput]::SendInput(1, [OcodeInput+KEYBDINPUT[]]@($ki), [System.Runtime.InteropServices.Marshal]::SizeOf([OcodeInput+KEYBDINPUT])) | Out-Null
        }
    }
}
