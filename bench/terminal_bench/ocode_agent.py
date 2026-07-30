"""Terminal-Bench adapter for ocode.

Runs ocode headless inside each task container and reports the run's real
token cost through TB's own AgentResult, whose base implementation hardcodes
zeros.
"""

import logging
import shlex
from pathlib import Path

from terminal_bench.agents.installed_agents.abstract_installed_agent import (
    AbstractInstalledAgent,
)
from terminal_bench.terminal.models import TerminalCommand

from bench.terminal_bench.hostenv import resolve_api_key, resolve_binary
from bench.terminal_bench.usage import parse_run_usage

logger = logging.getLogger(__name__)

CONTAINER_LOG = "/agent-logs/ocode-run.jsonl"
CONTAINER_ERR = "/agent-logs/ocode-run.err"
DIST_DIR = Path(__file__).parent / "dist"


class OcodeAgent(AbstractInstalledAgent):
    @staticmethod
    def name() -> str:
        return "ocode"

    def __init__(self, model_name: str, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._model_name = model_name
        # Resolve both host prerequisites now so a misconfigured host fails
        # before Docker spins up.
        self._api_key = resolve_api_key()
        self._binary = resolve_binary(DIST_DIR)

    @property
    def _env(self) -> dict[str, str]:
        return {
            "OPENCODE_API_KEY": self._api_key,
            "OCODE_MODEL": self._model_name,
        }

    @property
    def _install_agent_script_path(self) -> Path:
        return self._get_templated_script_path("ocode-setup.sh.j2")

    def _run_agent_commands(self, instruction: str) -> list[TerminalCommand]:
        escaped = shlex.quote(instruction)
        return [
            TerminalCommand(
                command=(
                    f'ocode run -yolo -format json -m "$OCODE_MODEL" '
                    f"-p {escaped} > {CONTAINER_LOG} 2> {CONTAINER_ERR}"
                ),
                min_timeout_sec=0.0,
                max_timeout_sec=float("inf"),
                block=True,
                append_enter=True,
            ),
        ]

    def perform_task(self, instruction, session, logging_dir=None):
        # The binary must land in the container before the install script runs.
        session.copy_to_container(
            self._binary,
            container_dir="/installed-agent",
            container_filename="ocode",
        )

        result = super().perform_task(instruction, session, logging_dir)

        if logging_dir is None:
            logger.warning(
                "no logging_dir given; cannot report ocode token usage"
            )
            return result

        usage = parse_run_usage(Path(logging_dir) / "ocode-run.jsonl")
        if usage is None:
            # Deliberately leave the base class's zeros in place and say so.
            # Inventing a number here would make a crashed run look cheap.
            logger.warning(
                "no usage event for this task; token cost is unknown"
            )
            return result

        result.total_input_tokens = usage.input_tokens
        result.total_output_tokens = usage.output_tokens
        return result
