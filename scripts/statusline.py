#!/usr/bin/env python3
"""
Agent Dashboard statusline — prints a one-line summary of running agents.

Usage:
    python3 scripts/statusline.py
    python3 scripts/statusline.py --format json
    python3 scripts/statusline.py --port 13120 --timeout 0.5

Options:
    --port PORT       Dashboard HTTP port (default: 13120)
    --timeout SECS    Request timeout in seconds (default: 0.5)
    --format FORMAT   Output format: text (default) or json

Environment:
    DASHBOARD_API_URL   Base URL of the dashboard (default: http://127.0.0.1:<port>)
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

import argparse
import json
import os
import urllib.error
import urllib.request
from typing import Any


def fetch_agents(base_url: str, token: str, timeout: float) -> list[dict[str, Any]]:
    url = f"{base_url}/api/agents"
    req = urllib.request.Request(url)
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read().decode())
    except (urllib.error.URLError, TimeoutError, OSError, json.JSONDecodeError):
        return []
    except Exception:
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
    if s["total"] == 0:
        return ""
    tok_k = s["total_tokens"] / 1000
    cost = s["cost_per_hour"]
    return f"⚡ {s['active']} active | ${cost:.2f}/h | {tok_k:.0f}K tok"


def get_status(port: int, timeout: float, fmt: str) -> str:
    base_url = os.environ.get("DASHBOARD_API_URL", f"http://127.0.0.1:{port}")
    token = os.environ.get("DASHBOARD_API_TOKEN", "")
    agents = fetch_agents(base_url, token, timeout)
    if not agents:
        return ""
    summary = summarize(agents)
    if fmt == "json":
        return json.dumps(summary)
    return format_statusline(summary)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Claude agent status for shell prompt"
    )
    parser.add_argument(
        "--port", type=int, default=13120, help="Dashboard port (default: 13120)"
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=0.5,
        help="Request timeout in seconds (default: 0.5)",
    )
    parser.add_argument(
        "--format",
        choices=["text", "json"],
        default="text",
        help="Output format: text (default) or json",
    )
    args = parser.parse_args()
    print(get_status(args.port, args.timeout, args.format), end="")


if __name__ == "__main__":
    main()
