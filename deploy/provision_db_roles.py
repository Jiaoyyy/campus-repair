"""Prepare runtime database accounts after running the target image's API migration."""

import argparse
import os
from pathlib import Path
import re
import secrets
import subprocess
import tempfile


API = {
    "SELECT": (
        "schema_migrations", "organizations", "users", "technician_categories",
        "agents", "agent_materials", "agent_skills", "agent_tools", "skills",
        "sessions", "conversations", "messages", "message_skill_runs",
        "assistant_business_results", "repair_drafts", "idempotency_records",
        "audit_events", "work_orders", "work_order_events", "repair_records",
        "work_order_recurrences", "work_order_visit_context", "work_order_visits",
        "work_order_media", "knowledge_materials", "knowledge_chunks", "index_jobs",
    ),
    "INSERT": (
        "agent_materials", "agent_skills", "agent_tools", "audit_events",
        "sessions", "conversations", "messages", "message_skill_runs",
        "assistant_business_results", "repair_drafts", "idempotency_records",
        "work_orders", "work_order_events", "repair_records", "work_order_recurrences",
        "work_order_visit_context", "work_order_visits", "work_order_media",
        "knowledge_materials", "index_jobs",
    ),
    "UPDATE": (
        "agents", "skills", "conversations", "messages", "repair_drafts",
        "idempotency_records", "work_orders", "work_order_visit_context",
        "work_order_visits", "knowledge_materials",
    ),
    # MySQL requires a write privilege for the row lock when serializing bookings.
    "UPDATE (id)": ("users",),
    "DELETE": ("agent_materials", "agent_skills", "agent_tools", "sessions", "idempotency_records"),
}

INDEX = {
    "SELECT": (
        "knowledge_materials", "index_jobs", "knowledge_generations", "knowledge_chunks",
        "vector_cleanup_jobs",
    ),
    "INSERT": ("knowledge_generations", "knowledge_chunks", "vector_cleanup_jobs", "audit_events"),
    "UPDATE": ("knowledge_materials", "index_jobs", "knowledge_generations", "knowledge_chunks"),
    "DELETE": ("vector_cleanup_jobs",),
}

MCP = {
    "SELECT": (
        "sessions", "agents", "agent_tools", "agent_skills", "skills", "agent_materials",
        "work_orders", "work_order_events", "repair_records", "technician_categories",
        "work_order_visit_context", "work_order_visits", "knowledge_materials",
        "knowledge_chunks", "mcp_call_counts",
    ),
    "SELECT (id, org_id, username, display_name, role, enabled)": ("users",),
    "INSERT": ("audit_events", "mcp_call_counts"),
    "UPDATE": ("mcp_call_counts",),
    "DELETE": ("mcp_call_counts",),
}

POLICIES = {
    "campus_api_runtime": ("DB_API_RUNTIME_PASSWORD", API),
    "campus_index_runtime": ("DB_INDEX_RUNTIME_PASSWORD", INDEX),
    "campus_mcp_runtime": ("DB_MCP_RUNTIME_PASSWORD", MCP),
}


def provision_passwords(path):
    source = path.read_text()
    passwords = {}
    additions = []
    for key, _ in POLICIES.values():
        existing = re.findall(rf"^{key}=(.*)$", source, re.MULTILINE)
        if len(existing) > 1 or (existing and not re.fullmatch(r"[0-9a-f]{64}", existing[0])):
            raise SystemExit(f"{key} must be a single generated 64-character hex value")
        if existing:
            passwords[key] = existing[0]
        else:
            passwords[key] = secrets.token_hex(32)
            additions.append(f"{key}={passwords[key]}")
        if key in os.environ and os.environ[key] != passwords[key]:
            raise SystemExit(f"Shell variable {key} conflicts with the selected env file")
    if additions:
        fd, name = tempfile.mkstemp(prefix=".env.db-roles-", dir=path.parent)
        temporary = Path(name)
        try:
            with os.fdopen(fd, "w") as output:
                output.write(source.rstrip("\n") + "\n\n" + "\n".join(additions) + "\n")
            temporary.replace(path)
        finally:
            temporary.unlink(missing_ok=True)
    return passwords


def grant_set(lines):
    """Normalize MySQL grant order and column order for exact account verification."""
    grants = {}
    for line in lines:
        match = re.fullmatch(r"GRANT (.+) ON (.+) TO `[a-z_]+`@`%`", line)
        if not match:
            raise SystemExit("Unexpected effective grant on a runtime database account")
        scope = match[2]
        privileges = set()
        for item in re.split(r",\s*(?![^()]*\))", match[1]):
            parts = item.partition("(")
            columns = tuple(sorted(c.strip().strip("`") for c in parts[2].rstrip(")").split(",") if c.strip()))
            privileges.add((parts[0].strip(), columns))
        grants.setdefault(scope, set()).update(privileges)
    return grants


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env-file", default=".env", type=Path)
    parser.add_argument("--project")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    path = args.env_file.resolve()
    compose = ["docker", "compose", "--env-file", str(path), "-f", "compose.yaml"]
    if args.project:
        compose += ["-p", args.project]

    def execute(mysql_command, data):
        return subprocess.run(
            [*compose, "exec", "-T", "db", "sh", "-c", mysql_command],
            input=data, text=True, capture_output=True, cwd=root, timeout=35,
        )

    def root_sql(query, purpose):
        result = execute(
            'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql --protocol=socket -u root '
            '--database="$MYSQL_DATABASE" --skip-column-names --batch --raw',
            query + "\n",
        )
        if result.returncode:
            raise SystemExit(f"Database role setup failed while {purpose}; verify DB readiness and run api migrate first")
        return result.stdout.strip()

    database = root_sql("SELECT DATABASE();", "identifying the project database")
    if not re.fullmatch(r"[A-Za-z0-9_]+", database):
        raise SystemExit("Database name must contain only letters, digits, or underscores")
    applied = root_sql("SELECT COUNT(*) FROM schema_migrations WHERE version BETWEEN 1 AND 10;", "checking the migration")
    if applied != "10":
        raise SystemExit("Run the target version's api migrate command before provisioning database roles")
    for user in POLICIES:
        # A localhost or more specific host entry could silently select a different account.
        hosts = root_sql(f"SELECT Host FROM mysql.user WHERE User='{user}';", "checking the dedicated database account")
        if hosts not in ("", "%"):
            raise SystemExit(f"Unexpected host entries for {user}; inspect the account before continuing")
    passwords = provision_passwords(path)

    for user, (environment, policy) in POLICIES.items():
        password = passwords[environment]
        account = f"`{user}`@`%`"
        root_sql(f"CREATE USER IF NOT EXISTS {account} IDENTIFIED BY '{password}';", f"creating {user}")
        grants = [f"GRANT {priv} ON `{database}`.`{table}` TO {account}" for priv, tables in policy.items() for table in tables]
        expected = grant_set([f"GRANT USAGE ON *.* TO {account}", *grants])
        show = f"SHOW GRANTS FOR {account};"
        current = grant_set(root_sql(show, f"checking {user}").splitlines())
        if any(not permissions <= expected.get(scope, set()) for scope, permissions in current.items()):
            raise SystemExit(f"Unexpected grants on {user}; review them before modifying the account")
        root_sql(";\n".join(grants) + ";", f"granting permissions to {user}")
        if grant_set(root_sql(show, f"verifying {user}").splitlines()) != expected:
            raise SystemExit(f"Effective grants differ for {user}")
        result = execute(
            'IFS= read -r role_password && MYSQL_PWD="$role_password" mysql --protocol=tcp '
            f'-h 127.0.0.1 -u {user} --batch -N -e "SELECT 1"',
            password + "\n",
        )
        if result.returncode or result.stdout.strip() != "1":
            raise SystemExit(f"Saved password does not authenticate {user}; check the deployment credential")
    print("PASS: database runtime accounts and grants verified")


if __name__ == "__main__":
    main()
