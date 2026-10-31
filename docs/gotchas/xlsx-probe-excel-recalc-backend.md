---
type: Gotcha
title: xlsx probe recalculates through Microsoft Excel (macOS gotchas)
description: The xlsx probe recalculates via real Microsoft Excel (AppleScript on macOS, COM on Windows, raises elsewhere). Sandboxed Mac Excel hangs on file access outside its container and goes stale after Save As.
resource: ""
tags: [xlsx, excel, kaizen, probe, macos]
timestamp: 2026-10-31T07:45:57Z
status: active
---
# xlsx probe recalculates through Microsoft Excel

The xlsx Kaizen probe (`docs/okf/xlsx/probe/`) checks a model's saved workbook by recalculating it and reading the results. The recalculation is done by real Microsoft Excel. The LibreOffice backend was removed.

## Current backend

`lib.py::recalc(path, outdir)` dispatches on `sys.platform`:

- `darwin`: runs `osascript excel_recalc.applescript <src> <dst>`. The script opens the copy, runs `calculate full`, saves it as Excel XML under the new name, and closes that copy by the new name. The input is never saved.
- `win32`: runs `powershell -File excel_recalc.ps1 -Src <src> -Dst <dst>` (COM). This script has never been run on Windows.
- anything else: raises `RuntimeError`. Linux and CI cannot run the probe.

Recalculations are serialised. Both platforms use a non-blocking acquire in a retry loop with one 900 s deadline (`EXCEL_LOCK_TIMEOUT`); exceeding it raises `RecalcError`. This keeps a Windows waiter from failing after ~10 s while a macOS waiter blocks forever.

The LibreOffice backend (`soffice --headless --convert-to xlsx`) is a TODO in `TODO.md` under "xlsx Kaizen probe: Excel-first recalculation (2026-10-31)". Its original code is in commit `9bf4c1b3`. LibreOffice 24.2 cannot evaluate `XLOOKUP` or dynamic arrays, so scores will differ between backends.

## macOS gotchas

1. **Sandboxed Excel hangs.** Excel asks for file access to any path outside its container. Nobody answers that prompt in a headless run, so the AppleEvent call times out (`-1712`). `recalc` therefore copies every input into `~/xlsx-probe-excel` and that folder is granted once, with one "Grant Access" click.
   **The grant is per folder, so both the staged input and the processed output live in `~/xlsx-probe-excel` on BOTH platforms** — Windows no longer hands Excel the caller's artifact. Names are unique per CALL (`in-<random>.xlsx` / `out-<random>.xlsx`), and both are removed afterwards, so a workbook left open by an earlier failure can never be reopened or clobbered by the next recalculation (check.py recalcs 2+ times per task).
   The "one Grant Access click" grant is per folder and survives across runs, but **restarting Excel can ask again**; an unanswered prompt surfaces as an `ENV-FAIL` (`-1712`), not a model failure.
2. **Writes into `~/Library/Containers/com.microsoft.Excel` are blocked** by macOS. Do not stage files there.
3. **Workbook reference goes stale after Save As.** After `save workbook as`, the original AppleScript workbook reference no longer resolves (`-1728`). Close by the new file name, as `excel_recalc.applescript` does.

## Excel function behaviour

- `IFNA` written by openpyxl without the `_xlfn.` prefix returns `#NAME?` in Excel. Write `_xlfn.IFNA(...)`. The probe README lists the same prefix rule for `MAXIFS`, `CONCAT`, `TEXTJOIN` and `IFS`; only `IFNA` was re-checked against Excel.

## Verification

- `selftest.sh` on the Excel backend: 28/28 ok. 13 correct references PASS and 15 cover-up or naive variants FAIL. Verified 2026-10-31.

## Closed-book baseline (not the live probe)

`opencode-go/deepseek-v4.1-flash`, 2026-10-31, 38 closed-book xlsx questions:

- Stack score 97.4% (89.6 / 92). No tag is below the 0.9 threshold. Lowest: dates-times 0.93, charts 0.95. No derived skill is warranted.
- Scorecard: `docs/okf/xlsx/scores/deepseek-v4.1-flash.md`. Answers: `docs/okf/xlsx/answers/deepseek-v4.1-flash.md`.
- The live probe (Phase B) has not been run yet.

## Limits

- The probe runs `ocode run -yolo`. Headless `ocode run` does not expose the `computer` or `window` tools (see `docs/computer-use.md`). Those tools exist only in TUI and web/server sessions, so a probe run cannot drive Excel through the GUI.
- The Windows COM path is untested (the PowerShell script has never been run on Windows).

## Failure classification

`check.py` prints a final `ENV-FAIL <reason>` and exits 3 when Excel cannot run (missing Excel, an unanswered access prompt, a timeout, or a lock timeout). `summarize.py` excludes those runs from every denominator and reports them separately, so a broken environment cannot masquerade as a model failure (PLAN.md §6 separates model / environment / checker failures).

## AppleScript lifecycle

The AppleScript opens the workbook INSIDE its `try`, closes inside the same `try`, best-effort closes in the error handler, and restores `display alerts` to true. Reason: a stranded workbook used to break every later recalculation in the same process (`check.py` recalcs 2+ times per task), and the shared Excel instance must not be left with alerts muted.

## PowerShell script (untested on Windows)

The PowerShell script takes named `-Src` / `-Dst` (lib.py passes them), null-guards and releases `$wb`, logs each teardown failure to stderr, and runs `[GC]::Collect()` + `WaitForPendingFinalizers()` so no orphan `EXCEL.EXE` survives. It is still UNTESTED on Windows.

## Runtime error contract

`RecalcError` subclasses `RuntimeError`, so callers can catch the whole family. The `-1728` stale-reference gotcha (workbook reference goes stale after Save As) is handled by closing by the new file name.