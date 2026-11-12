"""Local upgrade backup helper. Never echo config or PostgreSQL diagnostics."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys


def database_config(runtime):
    # The local installer writes scalar YAML fields. Reject unsupported syntax.
    config = {}
    section = None
    for line in (runtime / "data/config.yaml").read_text(encoding="utf-8-sig").splitlines():
        if line and not line[0].isspace() and not line.startswith("#"):
            section = line.split(":", 1)[0]
        if section != "database":
            continue
        match = re.fullmatch(r"\s+(host|port|user|password|dbname|sslmode):\s*(.*)", line)
        if not match:
            continue
        value = match[2].strip()
        if value.startswith('"'):
            value = json.loads(value)
        elif value.startswith("'"):
            if not value.endswith("'"):
                raise ValueError("Unsupported database config quoting")
            value = value[1:-1].replace("''", "'")
        elif value and (value[0] in "{[&*!|>" or " #" in value):
            raise ValueError("Unsupported database config syntax")
        if not isinstance(value, str) or "\n" in value or "\r" in value:
            raise ValueError("Invalid database scalar")
        config[match[1]] = value
    if not all(config.get(key) for key in ("host", "port", "user", "dbname")):
        raise ValueError("Database config is incomplete")
    return config


def execute(pg_bin, program, args, env):
    result = subprocess.run([str(pg_bin / program), *args], env=env,
                            capture_output=True, encoding="utf-8", errors="replace")
    if result.returncode:
        raise RuntimeError(f"{program} failed; database diagnostics suppressed")
    return result.stdout


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=["inspect", "backup"])
    parser.add_argument("--runtime", required=True, type=Path)
    parser.add_argument("--pg-bin", required=True, type=Path)
    parser.add_argument("--directory", type=Path)
    args = parser.parse_args()
    runtime = args.runtime.resolve()
    config = database_config(runtime)
    env = os.environ.copy()
    env.update(PGPASSWORD=config.get("password", ""), PGCONNECT_TIMEOUT="5",
               PGCLIENTENCODING="UTF8", PGSSLMODE=config.get("sslmode", "disable"))
    env.pop("PGOPTIONS", None)
    connection = ["-h", config["host"], "-p", config["port"],
                  "-U", config["user"], "-d", config["dbname"]]
    if args.action == "backup":
        if args.directory is None:
            raise ValueError("Backup directory is required")
        folder = args.directory.resolve()
        if not folder.is_relative_to(runtime / "backups"):
            raise ValueError("Backup must stay under runtime/backups")
        folder.mkdir(parents=True, exist_ok=True)
        archive = folder / "database.dump"
        if archive.exists():
            raise FileExistsError("Refusing to overwrite database backup")
        execute(args.pg_bin, "pg_dump.exe", ["-w", *connection, "-Fc", "--file", str(archive)], env)
        listing = execute(args.pg_bin, "pg_restore.exe", ["--list", str(archive)], env)
        if not archive.stat().st_size or "TABLE DATA" not in listing:
            raise RuntimeError("Database backup validation failed")
    env["PGOPTIONS"] = "-c default_transaction_read_only=on -c statement_timeout=10000"
    snapshot = execute(args.pg_bin, "psql.exe", ["-X", "-w", *connection, "-t", "-A",
                       "-v", "ON_ERROR_STOP=1", "-c",
                       "SELECT COALESCE(json_agg(filename ORDER BY filename), '[]'::json) FROM schema_migrations"], env)
    migrations = json.loads(snapshot)
    if not isinstance(migrations, list) or not all(isinstance(item, str) for item in migrations):
        raise ValueError("Invalid database migration snapshot")
    result = {"migrations": migrations}
    if args.action == "backup":
        (folder / "database-before.json").write_text(json.dumps(result, indent=2), encoding="utf-8")
    print(json.dumps(result))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        # Even parsing exceptions can contain private scalar values.
        print("Database preflight/backup failed; credentials and diagnostics suppressed.", file=sys.stderr)
        sys.exit(1)
