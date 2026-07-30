"""Parse token usage from an ocode headless run log.

Deliberately free of any terminal_bench import so it can be tested without
Docker or the harness.
"""

import json
import logging
from dataclasses import dataclass
from pathlib import Path

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class RunUsage:
    input_tokens: int
    output_tokens: int
    model_calls: int


def parse_run_usage(log_path) -> "RunUsage | None":
    """Return the last usage event in an ocode JSONL run log.

    Returns None when the log is missing or has no usage event. A run whose
    cost is unknown must stay unknown -- returning zeros here would make a
    crashed run look free and quietly drag down any token average.
    """
    path = Path(log_path)
    try:
        raw = path.read_text()
    except OSError as err:
        logger.warning("could not read ocode run log %s: %s", path, err)
        return None

    found = None
    for line in raw.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError as err:
            logger.debug("skipping malformed line in %s: %s", path, err)
            continue
        if isinstance(event, dict) and event.get("type") == "usage":
            found = event

    if found is None:
        logger.warning("no usage event found in ocode run log %s", path)
        return None

    return RunUsage(
        input_tokens=int(found["input_tokens"]),
        output_tokens=int(found["output_tokens"]),
        model_calls=int(found["model_calls"]),
    )
