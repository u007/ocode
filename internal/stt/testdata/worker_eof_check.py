"""Test the REAL stt_worker.py's stdin-EOF contract with stubbed dependencies.

The worker must exit when its stdin reaches EOF. That is the orphan guard on
macOS and the BSDs, where Go's syscall package has no Pdeathsig: if ocode is
killed, the pipe closes, and the worker must notice and exit rather than hold
its model forever.

This runs the real embedded script (not a hand-written fake) with
huggingface_hub and onnx_asr stubbed out, so the request loop under test is
the production one.
"""
import os
import subprocess
import sys
import tempfile
import textwrap

HERE = os.path.dirname(os.path.abspath(__file__))
# The worker script sits one directory up, next to the Go package.
SCRIPT = os.path.join(os.path.dirname(HERE), "stt_worker.py")


def main() -> int:
    if not os.path.exists(SCRIPT):
        print("FAIL: worker script not found at", SCRIPT)
        return 1

    tmp = tempfile.mkdtemp(prefix="ocode-stt-eof-")
    # A stub package dir that shadows huggingface_hub and onnx_asr.
    stubs = os.path.join(tmp, "stubs")
    os.makedirs(stubs)
    with open(os.path.join(stubs, "huggingface_hub.py"), "w") as f:
        f.write("def snapshot_download(repo, local_dir=None, local_files_only=False):\n"
                "    import os\n"
                "    os.makedirs(local_dir, exist_ok=True)\n"
                "    return local_dir\n")
    with open(os.path.join(stubs, "onnx_asr.py"), "w") as f:
        f.write(textwrap.dedent('''
            class _M:
                def recognize(self, path):
                    return "stub transcript"
            def load_model(name, path=None, **kw):
                return _M()
        '''))

    model_dir = os.path.join(tmp, "model")
    os.makedirs(model_dir)
    env = dict(os.environ)
    env["PYTHONPATH"] = stubs
    env["ORT_DISABLE_TELEMETRY"] = "1"

    # The worker normally runs under `python -I` (isolated mode) so nothing in
    # the working directory can shadow onnx_asr. -I also ignores PYTHONPATH,
    # so the stubs are injected by dropping -I here. The code under test is
    # the request loop, which is identical either way.
    proc = subprocess.Popen(
        [sys.executable, SCRIPT, "nemo-parakeet-tdt-0.6b-v3", "repo", model_dir],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        env=env, text=True,
    )
    try:
        # The worker must announce itself before it starts serving.
        line = proc.stdout.readline()
        if '"op": "ready"' not in line and '"op":"ready"' not in line:
            print("FAIL: no ready handshake, got:", line.strip())
            return 1
        print("handshake:", line.strip())

        # Now close stdin, which is what a dead parent does.
        proc.stdin.close()
        try:
            proc.wait(timeout=15)
        except subprocess.TimeoutExpired:
            proc.kill()
            print("FAIL: the worker did not exit after stdin reached EOF")
            return 1
        print("worker exited with code", proc.returncode)
        if proc.returncode != 0:
            print("FAIL: non-zero exit")
            return 1
    finally:
        if proc.poll() is None:
            proc.kill()
    print("PASS: the worker exits when stdin closes")
    return 0


if __name__ == "__main__":
    sys.exit(main())
