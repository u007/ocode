import json
import shlex
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from bench.terminal_bench.ocode_agent import OcodeAgent


def _make_agent(tmp_dist):
    with mock.patch(
        "bench.terminal_bench.ocode_agent.resolve_api_key", return_value="sk-test"
    ), mock.patch(
        "bench.terminal_bench.ocode_agent.resolve_binary",
        return_value=Path(tmp_dist) / "ocode-linux-arm64",
    ):
        return OcodeAgent(model_name="opencode-go/deepseek-v4-flash")


class OcodeAgentEnvTest(unittest.TestCase):
    def test_env_carries_key_and_model(self):
        agent = _make_agent(tempfile.mkdtemp())
        env = agent._env
        self.assertEqual(env["OPENCODE_API_KEY"], "sk-test")
        self.assertEqual(env["OCODE_MODEL"], "opencode-go/deepseek-v4-flash")

    def test_run_command_is_headless_json_and_redirected(self):
        agent = _make_agent(tempfile.mkdtemp())
        commands = agent._run_agent_commands("fix the failing test")
        self.assertEqual(len(commands), 1)
        command = commands[0].command
        self.assertIn("ocode run", command)
        self.assertIn("-yolo", command)
        self.assertIn("-format json", command)
        self.assertIn("/agent-logs/ocode-run.jsonl", command)
        self.assertIn(shlex.quote("fix the failing test"), command)


class OcodeAgentTokenReportingTest(unittest.TestCase):
    def _run_with_log(self, log_lines):
        logging_dir = Path(tempfile.mkdtemp())
        if log_lines is not None:
            (logging_dir / "ocode-run.jsonl").write_text(
                "\n".join(json.dumps(line) for line in log_lines) + "\n"
            )

        agent = _make_agent(tempfile.mkdtemp())
        session = mock.MagicMock()
        base_result = mock.MagicMock(
            total_input_tokens=0, total_output_tokens=0, failure_mode="none"
        )
        with mock.patch(
            "bench.terminal_bench.ocode_agent."
            "AbstractInstalledAgent.perform_task",
            return_value=base_result,
        ):
            return agent.perform_task("task", session, logging_dir), session

    def test_reports_real_token_counts(self):
        result, _ = self._run_with_log([
            {"type": "text", "part": {"type": "text", "text": "hi"}},
            {"type": "usage", "input_tokens": 3000, "output_tokens": 700,
             "model_calls": 9},
        ])
        self.assertEqual(result.total_input_tokens, 3000)
        self.assertEqual(result.total_output_tokens, 700)

    def test_leaves_zeros_when_usage_missing(self):
        result, _ = self._run_with_log(None)
        self.assertEqual(result.total_input_tokens, 0)
        self.assertEqual(result.total_output_tokens, 0)

    def test_preserves_failure_mode_from_base_class(self):
        result, _ = self._run_with_log([
            {"type": "usage", "input_tokens": 1, "output_tokens": 1,
             "model_calls": 1},
        ])
        self.assertEqual(result.failure_mode, "none")

    def test_copies_binary_into_container(self):
        _, session = self._run_with_log([
            {"type": "usage", "input_tokens": 1, "output_tokens": 1,
             "model_calls": 1},
        ])
        session.copy_to_container.assert_called_once()
        kwargs = session.copy_to_container.call_args.kwargs
        self.assertEqual(kwargs["container_filename"], "ocode")


if __name__ == "__main__":
    unittest.main()
