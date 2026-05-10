#!/usr/bin/env python3
"""
Agent Dashboard statusline — prints a one-line summary of running agents.

Usage:
    python3 scripts/statusline.py
    python3 scripts/statusline.py --format json

Environment:
    DASHBOARD_API_URL   Base URL of the dashboard (default: http://127.0.0.1:13120)
    DASHBOARD_API_TOKEN Bearer token if auth is enabled (optional)

Shell integration (zsh) — add to ~/.zshrc:
    _agent_status() {
        local out
        out=$(python3 /path/to/agent-dashboard/scripts/statusline.py 2>/dev/null)
        [[ -n "$out" ]] && echo " [$out]"
    }
    PROMPT='%n@%m %~$(_agent_status) %# '

Shell integration (bash) — add to ~/.bashrc:
    _agent_status() {
        python3 /path/to/agent-dashboard/scripts/statusline.py 2>/dev/null
    }
    PROMPT_COMMAND='export PS1="\\u@\\h \\w [$(_agent_status)] \\$ "'
"""

import json
import os
import sys
import urllib.request
import urllib.error
from typing import Any

BASE_URL = os.environ.get("DASHBOARD_API_URL", "http://127.0.0.1:13120")
TOKEN = os.environ.get("DASHBOARD_API_TOKEN", "")


def fetch_agents() -> list[dict[str, Any]]:
    url = f"{BASE_URL}/api/agents"
    req = urllib.request.Request(url)
    if TOKEN:
        req.add_header("Authorization", f"Bearer {TOKEN}")
    try:
        with urllib.request.urlopen(req, timeout=2) as resp:
            return json.loads(resp.read().decode())
    except (urllib.error.URLError, json.JSONDecodeError):
        return []


def summarize(agents: list[dict[str, Any]]) -> dict[str, Any]:
    active = sum(1 for a in agents if a.get("status") == "active")
    cost_per_hour = sum(
        a.get("costEstimate", 0) for a in agents if a.get("status") == "active"
    )
    total_tokens = sum(
        sum(a.get("tokenUsage", {}).values()) for a in agents
    )
    return {
        "active": active,
        "total": len(agents),
        "cost_per_hour": cost_per_hour,
        "total_tokens": total_tokens,
    }


def format_statusline(s: dict[str, Any]) -> str:
    tok_k = s["total_tokens"] / 1000
    cost = s["cost_per_hour"]
    return f"⚡ {s['active']} active | ${cost:.2f}/h | {tok_k:.0f}K tok"


def main() -> None:
    use_json = (
        "--format" in sys.argv
        and sys.argv.index("--format") + 1 < len(sys.argv)
        and sys.argv[sys.argv.index("--format") + 1] == "json"
    )
    agents = fetch_agents()
    summary = summarize(agents)
    if use_json:
        print(json.dumps(summary))
    else:
        print(format_statusline(summary))


if __name__ == "__main__":
    main()
