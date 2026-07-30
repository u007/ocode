import json
import tempfile
import unittest
from pathlib import Path

from bench.terminal_bench.usage import RunUsage, parse_run_usage


class ParseRunUsageTest(unittest.TestCase):
    def _write(self, lines):
        tmp = tempfile.NamedTemporaryFile(
            mode="w", suffix=".jsonl", delete=False
        )
        for line in lines:
            tmp.write(json.dumps(line) + "\n")
        tmp.close()
        return Path(tmp.name)

    def test_reads_trailing_usage_event(self):
        path = self._write([
            {"type": "text", "part": {"type": "text", "text": "working"}},
            {"type": "tool_use", "part": {"tool": "bash"}},
            {
                "type": "usage",
                "input_tokens": 2000,
                "output_tokens": 450,
                "total_tokens": 2450,
                "model_calls": 7,
            },
        ])
        usage = parse_run_usage(path)
        self.assertEqual(usage, RunUsage(2000, 450, 7))

    def test_returns_none_when_no_usage_event(self):
        path = self._write([
            {"type": "text", "part": {"type": "text", "text": "crashed"}},
        ])
        self.assertIsNone(parse_run_usage(path))

    def test_returns_none_when_file_missing(self):
        self.assertIsNone(parse_run_usage(Path("/nonexistent/ocode-run.jsonl")))

    def test_ignores_malformed_lines(self):
        path = Path(
            tempfile.NamedTemporaryFile(suffix=".jsonl", delete=False).name
        )
        path.write_text(
            "not json at all\n"
            + json.dumps({
                "type": "usage",
                "input_tokens": 10,
                "output_tokens": 2,
                "model_calls": 1,
            })
            + "\n"
        )
        self.assertEqual(parse_run_usage(path), RunUsage(10, 2, 1))

    def test_last_usage_event_wins(self):
        path = self._write([
            {"type": "usage", "input_tokens": 1, "output_tokens": 1,
             "model_calls": 1},
            {"type": "usage", "input_tokens": 99, "output_tokens": 9,
             "model_calls": 3},
        ])
        self.assertEqual(parse_run_usage(path), RunUsage(99, 9, 3))


if __name__ == "__main__":
    unittest.main()
