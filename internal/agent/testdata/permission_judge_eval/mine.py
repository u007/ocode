#!/usr/bin/env python3
"""Mine past auto-permission judge decisions into eval cases.

Reads the durable judge log (permission-judge.log) and, for every bash decision
made in a real session, looks the call up in that session's database to learn
what happened next: whether the command ran or the user denied it. That outcome
is the label the eval grades against.

    python3 mine.py > mined.json

The output holds real commands from every project the judge ran in, so
mined.json is gitignored. Review it before sharing.
"""
import glob
import json
import os
import shutil
import sqlite3
import sys
import tempfile

DATA = os.path.expanduser("~/.local/share/opencode")
LOG = os.path.join(DATA, "logs", "permission-judge.log")


def log_rows():
    with open(LOG, errors="replace") as f:
        for line in f:
            start = line.find("{")
            if start < 0:
                continue
            try:
                row = json.loads(line[start:])
            except json.JSONDecodeError:
                continue
            # Unit tests write to the same log with a fake judge; only rows from
            # a real session carry a real score.
            if not str(row.get("session", "")).startswith("ses_2"):
                continue
            if row.get("tool") != "bash" or not row.get("command") or row.get("command_withheld"):
                continue
            yield row


def session_calls(session_id, scratch):
    """Every bash tool call in the session, in order, with its result text."""
    paths = glob.glob(os.path.join(DATA, "project", "*", "sessions", session_id + ".sqlite"))
    if not paths:
        return []
    # Open a copy: the live file may be locked by a running server.
    for src in glob.glob(paths[0] + "*"):
        shutil.copy(src, scratch)
    db = sqlite3.connect(os.path.join(scratch, os.path.basename(paths[0])))
    calls, results = [], {}
    for (data,) in db.execute("select data from messages order by seq"):
        msg = json.loads(data)
        if msg.get("role") == "tool":
            results[msg.get("tool_call_id")] = msg.get("content") or ""
        for tc in msg.get("tool_calls") or []:
            fn = tc.get("function") or {}
            if fn.get("name") != "bash":
                continue
            try:
                command = json.loads(fn.get("arguments") or "{}").get("command", "")
            except json.JSONDecodeError:
                continue
            calls.append({"id": tc.get("id"), "command": command.strip(), "used": False})
    db.close()
    for call in calls:
        call["result"] = results.get(call["id"])
    return calls


def user_decision(result):
    if result is None:
        return "unknown"
    if result.startswith("PERMISSION_ASK:"):
        return "unresolved"
    if result.startswith("denied:"):
        return "denied_by_user" if "denied by user" in result else "denied_other"
    return "ran"


def main():
    cases, calls_by_session = {}, {}
    with tempfile.TemporaryDirectory() as scratch:
        for row in log_rows():
            sid = row["session"]
            if sid not in calls_by_session:
                calls_by_session[sid] = session_calls(sid, scratch)
            logged = row["command"].strip()
            # The log records the command AFTER a leading `cd <dir> &&` was
            # folded away, so the original either equals it or ends with it.
            match = None
            for call in calls_by_session[sid]:
                if call["used"]:
                    continue
                if call["command"] == logged or (row.get("resolved_cd") and call["command"].endswith(logged)):
                    match = call
                    break
            if match is None:
                continue
            match["used"] = True
            decision = user_decision(match["result"])
            outcome = row["outcome"]
            if outcome == "granted":
                expect, source = "allow", "judge_granted"
            elif decision == "ran":
                expect, source = "allow", "user_approved"
            elif decision == "denied_by_user":
                expect, source = "ask", "user_denied"
            else:
                continue
            case = {
                "time": row["time"],
                "session": sid,
                "command": match["command"],
                "working_directory": row.get("working_directory", ""),
                "rule": row.get("rule", ""),
                "scope": row.get("scope", ""),
                "allow_destructive": row.get("allow_destructive", False),
                "expect": expect,
                "label_source": source,
                "logged": {
                    "outcome": outcome,
                    "choice": row.get("choice", ""),
                    "confidence": row.get("confidence", 0),
                    "p_allow": (row.get("probabilities") or {}).get("allow", 0),
                    "concern": row.get("concern", ""),
                    "floor": row.get("floor", 0),
                },
            }
            key = (case["working_directory"], case["command"])
            # One case per distinct command; keep its lowest-scoring occurrence.
            if key not in cases or case["logged"]["confidence"] < cases[key]["logged"]["confidence"]:
                cases[key] = case
    out = sorted(cases.values(), key=lambda c: c["time"])
    for n, case in enumerate(out, 1):
        case["id"] = "m%03d" % n
    json.dump({"cases": out}, sys.stdout, indent=1, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
