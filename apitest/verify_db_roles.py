"""Verify the effective database authorization of a hardened isolated project."""

import argparse
import json
from pathlib import Path
import re
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env-file", required=True, type=Path)
    parser.add_argument("--project", required=True)
    args = parser.parse_args()
    if not args.project.endswith("-check") or args.project == "campus-repair-review-check":
        parser.error("Use a dedicated test project")

    settings = dict(line.split("=", 1) for line in args.env_file.read_text().splitlines()
                    if "=" in line and not line.startswith("#"))
    compose = [
        "docker", "compose", "-p", args.project, "--env-file", str(args.env_file.resolve()),
        "-f", "compose.yaml", "-f", "compose.secure.yaml",
    ]
    root = Path(__file__).resolve().parents[1]
    roles = {
        "api": ("campus_api_runtime", "DB_API_RUNTIME_PASSWORD"),
        "worker": ("campus_index_runtime", "DB_INDEX_RUNTIME_PASSWORD"),
        "mcp": ("campus_mcp_runtime", "DB_MCP_RUNTIME_PASSWORD"),
    }
    for service, (account, key) in roles.items():
        container = subprocess.check_output([*compose, "ps", "-q", service], text=True, cwd=root).strip()
        state = json.loads(subprocess.check_output(["docker", "inspect", container], text=True))[0]
        env = dict(value.split("=", 1) for value in state["Config"]["Env"])
        assert env["DB_USER"] == account and env["DB_PASSWORD"] == settings[key], f"{service} runtime account"
        assert not any(k.startswith("SEED_") for k in env), f"{service} has bootstrap credentials"
        if service != "api":
            assert "CHAT_API_KEY" not in env and "SESSION_SECRET" not in env, f"{service} has API-only credentials"
        if service == "worker":
            assert "MCP_CALL_SECRET" not in env, "worker has MCP credentials"

    def sql(service, query, allowed):
        account, key = roles[service]
        command = (
            'IFS= read -r role_password && MYSQL_PWD="$role_password" mysql --protocol=tcp '
            f'-h 127.0.0.1 -u {account} --database="$MYSQL_DATABASE" --batch -N'
        )
        result = subprocess.run([*compose, "exec", "-T", "db", "sh", "-c", command],
                                input=settings[key] + "\n" + query + "\n", text=True,
                                capture_output=True, cwd=root, timeout=10)
        if allowed:
            assert result.returncode == 0, f"{service} expected an allowed statement"
        else:
            assert result.returncode and re.search(r"ERROR (1142|1143|1044)\b", result.stderr), (
                f"{service} expected denial from MySQL"
            )

    sql("api", "SELECT id FROM users LIMIT 1 FOR UPDATE;", True)
    sql("api", "UPDATE audit_events SET action=action WHERE FALSE;", False)
    sql("api", "CREATE TEMPORARY TABLE runtime_ddl_check (id INT);", False)
    sql("worker", "SELECT id FROM index_jobs LIMIT 1 FOR UPDATE SKIP LOCKED;", True)
    sql("worker", "SELECT id FROM work_orders LIMIT 0;", False)
    sql("worker", "SELECT token_hash FROM sessions LIMIT 0;", False)
    sql("mcp", "SELECT id FROM work_orders LIMIT 1 FOR SHARE;", True)
    sql("mcp", "SELECT password_hash FROM users LIMIT 0;", False)
    sql("mcp", "UPDATE work_orders SET version=version WHERE FALSE;", False)
    sql("mcp", "UPDATE mcp_call_counts SET used=used WHERE FALSE;", True)
    print("PASS: isolated runtime identities, secret boundaries and 10 SQL authorization checks")


if __name__ == "__main__":
    main()
