import json
import tempfile
import unittest
from pathlib import Path

from bench.terminal_bench.hostenv import resolve_api_key, resolve_binary


class ResolveApiKeyTest(unittest.TestCase):
    def _auth_file(self, payload):
        tmp = tempfile.NamedTemporaryFile(
            mode="w", suffix=".json", delete=False
        )
        json.dump(payload, tmp)
        tmp.close()
        return Path(tmp.name)

    def test_reads_key_from_auth_store(self):
        path = self._auth_file({"opencode-go": {"type": "api", "key": "sk-abc"}})
        self.assertEqual(resolve_api_key(auth_path=path, environ={}), "sk-abc")

    def test_environment_variable_wins(self):
        path = self._auth_file({"opencode-go": {"type": "api", "key": "sk-abc"}})
        self.assertEqual(
            resolve_api_key(
                auth_path=path, environ={"OPENCODE_API_KEY": "sk-env"}
            ),
            "sk-env",
        )

    def test_raises_when_no_credential_anywhere(self):
        path = self._auth_file({"openai": {"type": "api", "key": "sk-other"}})
        with self.assertRaises(RuntimeError) as ctx:
            resolve_api_key(auth_path=path, environ={})
        self.assertIn("opencode-go", str(ctx.exception))

    def test_raises_when_auth_file_missing(self):
        with self.assertRaises(RuntimeError):
            resolve_api_key(auth_path=Path("/nonexistent/auth.json"), environ={})


class ResolveBinaryTest(unittest.TestCase):
    def test_returns_matching_arch_binary(self):
        dist = Path(tempfile.mkdtemp())
        binary = dist / "ocode-linux-arm64"
        binary.write_text("#!/bin/sh\n")
        self.assertEqual(resolve_binary(dist, arch="arm64"), binary)

    def test_raises_with_build_instruction_when_absent(self):
        dist = Path(tempfile.mkdtemp())
        with self.assertRaises(RuntimeError) as ctx:
            resolve_binary(dist, arch="amd64")
        self.assertIn("make build-linux", str(ctx.exception))


if __name__ == "__main__":
    unittest.main()
