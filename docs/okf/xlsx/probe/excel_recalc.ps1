# usage: powershell -NoProfile -File excel_recalc.ps1 -Src <src.xlsx> -Dst <dst.xlsx>
# Opens src in Microsoft Excel (COM), forces a full calculation, saves a copy as dst, closes without touching src.
# UNTESTED: written alongside the macOS script, never run on Windows (see TODO.md).
param(
    [Parameter(Mandatory = $true)][string]$Src,
    [Parameter(Mandatory = $true)][string]$Dst
)
$ErrorActionPreference = 'Stop'
$code = 0
$xl = $null
$wb = $null
try {
    $xl = New-Object -ComObject Excel.Application
    $xl.Visible = $false
    $xl.DisplayAlerts = $false
    $wb = $xl.Workbooks.Open($Src, 0)
    $xl.CalculateFull()
    $wb.SaveAs($Dst, 51)   # xlOpenXMLWorkbook
    $wb.Close($false)
    $wb = $null
}
catch {
    $code = 1
    [Console]::Error.WriteLine("Excel recalculation failed: " + $_.Exception.Message)
    # Best effort: never leave the workbook open, or the next recalculation collides with it.
    if ($null -ne $wb) {
        try { $wb.Close($false) } catch { [Console]::Error.WriteLine("could not close the workbook: " + $_.Exception.Message) }
        $wb = $null
    }
}
finally {
    if ($null -ne $xl) {
        try { $xl.Quit() } catch { [Console]::Error.WriteLine("could not quit Excel: " + $_.Exception.Message) }
        try { [void][System.Runtime.InteropServices.Marshal]::ReleaseComObject($xl) } catch { [Console]::Error.WriteLine("could not release the Excel COM object: " + $_.Exception.Message) }
    }
    # An unreleased COM object leaves an EXCEL.EXE process (with a window) behind; force the finalizers.
    [GC]::Collect()
    [GC]::WaitForPendingFinalizers()
}
exit $code
