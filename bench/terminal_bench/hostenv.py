"""Resolve host-side prerequisites for the ocode Terminal-Bench adapter.

Both resolvers raise at adapter construction time so a misconfigured host
fails before Docker spins up, not after tokens have been spent.
"""

import json
import os
import platform
from pathlib import Path

PROVIDER_ID = "opencode-go"
ENV_VAR = "OPENCODE_API_KEY"
DEFAULT_AUTH_PATH = Path.home() / ".local" / "share" / "opencode" / "auth.json"


def resolve_api_key(auth_path=None, environ=None) -> str:
    """Return the opencode-go API key.

    Precedence matches auth.HydrateEnv (internal/auth/providers.go): an
    already-set environment variable wins over the stored credential.
    """
    environ = os.environ if environ is None else environ
    from_env = environ.get(ENV_VAR)
    if from_env:
        return from_env

    path = Path(DEFAULT_AUTH_PATH if auth_path is None else auth_path)
    try:
        store = json.loads(path.read_text())
    except OSError as err:
        raise RuntimeError(
            f"cannot read the opencode auth store at {path}: {err}. "
            f"Export {ENV_VAR} or log in with `ocode` first."
        ) from err
    except json.JSONDecodeError as err:
        raise RuntimeError(
            f"the opencode auth store at {path} is not valid JSON: {err}"
        ) from err

    entry = store.get(PROVIDER_ID) or {}
    key = entry.get("key") or entry.get("api_key") or entry.get("apiKey")
    if not key:
        raise RuntimeError(
            f"no {PROVIDER_ID} credential in {path} and {ENV_VAR} is unset. "
            f"Run `ocode` and authenticate the {PROVIDER_ID} provider."
        )
    return key


def resolve_binary(dist_dir, arch=None) -> Path:
    """Return the Linux ocode binary to copy into the task container."""
    if arch is None:
        machine = platform.machine().lower()
        arch = "arm64" if machine in ("arm64", "aarch64") else "amd64"

    binary = Path(dist_dir) / f"ocode-linux-{arch}"
    if not binary.is_file():
        raise RuntimeError(
            f"missing {binary}. Build it first with `make build-linux` and "
            f"copy ocode-linux-{arch} into {dist_dir}."
        )
    return binary
