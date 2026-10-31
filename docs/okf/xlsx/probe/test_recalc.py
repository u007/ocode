#!/usr/bin/env python3
"""Plumbing tests for lib.recalc that need NO Excel.

Run directly: `python3 test_recalc.py`. A fake backend (a shell script that copies its input)
stands in for Excel, so every failure path can be exercised on Linux/CI as well as macOS/Windows.

What this pins (each of these was a real defect):
  * the caller's workbook is never opened for writing (staged copies only);
  * staging names are unique per call, so a stranded workbook cannot collide with the next one;
  * both staging files are removed on success, failure, timeout and no-output;
  * a backend failure raises RecalcError carrying the backend's stderr, not a bare OSError;
  * a silent no-write raises RecalcError("produced no output"), not FileNotFoundError;
  * a timeout and a lock deadline raise RecalcError;
  * an unsupported platform is refused before anything is written;
  * out == input is refused instead of truncating the artifact.
"""
import pathlib
import sys
import tempfile

import lib

TMP = pathlib.Path(tempfile.mkdtemp(prefix="recalc-test-"))
SRC = TMP / "book.xlsx"
SRC.write_bytes(b"pretend workbook")
lib.STAGE_DIR = TMP / "stage"
REAL_BACKEND = lib._backend


def script(name, body):
    p = TMP / name
    p.write_text(body)
    p.chmod(0o755)
    return p


def use(backend_script):
    lib._backend = lambda: ["/bin/sh", str(backend_script)]


def staged():
    return sorted(p.name for p in lib.STAGE_DIR.iterdir())


def expect_error(fn, needle):
    try:
        fn()
    except lib.RecalcError as e:
        assert needle in str(e), f"expected {needle!r} in {e!r}"
        return
    raise AssertionError(f"expected RecalcError containing {needle!r}")


def main():
    ok = script("ok.sh", '#!/bin/sh\ncp "$1" "$2"\n')
    bad = script("bad.sh", '#!/bin/sh\necho "boom: excel said no" >&2\nexit 3\n')
    slow = script("slow.sh", '#!/bin/sh\nsleep 30\ncp "$1" "$2"\n')
    empty = script("empty.sh", "#!/bin/sh\nexit 0\n")

    use(ok)
    out = lib.recalc(SRC)
    assert out.read_bytes() == b"pretend workbook"
    assert SRC.read_bytes() == b"pretend workbook", "the caller's workbook was modified"
    assert staged() == [], staged()
    out2 = lib.recalc(SRC, outdir=TMP / "second")
    assert out2.exists() and staged() == [], staged()

    use(bad)
    expect_error(lambda: lib.recalc(SRC), "boom: excel said no")
    assert staged() == [], staged()

    use(empty)
    expect_error(lambda: lib.recalc(SRC), "produced no output")
    assert staged() == [], staged()

    use(slow)
    lib.EXCEL_RUN_TIMEOUT = 1
    expect_error(lambda: lib.recalc(SRC), "timed out")
    assert staged() == [], staged()

    lib._backend = REAL_BACKEND
    platform = sys.platform
    try:
        sys.platform = "linux"
        expect_error(lambda: lib.recalc(SRC), "no Microsoft Excel backend")
    finally:
        sys.platform = platform

    lib.HERE = TMP / "missing-probe-dir"
    try:
        expect_error(lambda: lib._backend(), "incomplete checkout")
    finally:
        lib.HERE = pathlib.Path(__file__).resolve().parent

    try:
        lib.recalc(SRC, outdir=SRC.parent)
    except ValueError as e:
        assert "overwrite its input" in str(e), e
    else:
        raise AssertionError("expected ValueError for out == input")

    with lib._excel_lock(timeout=5):
        try:
            with lib._excel_lock(timeout=0.3):
                raise AssertionError("expected the lock deadline to fire")
        except lib.RecalcError as e:
            assert "waiting for the Excel lock" in str(e), e

    print("recalc plumbing: all checks passed (no Excel needed)")


if __name__ == "__main__":
    main()
