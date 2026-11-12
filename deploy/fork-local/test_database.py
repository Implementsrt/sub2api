import contextlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("local_database", Path(__file__).with_name("database.py"))
database = importlib.util.module_from_spec(spec)
spec.loader.exec_module(database)


class BackupTests(unittest.TestCase):
    def setUp(self):
        fixture_root = Path(__file__).resolve().parents[2] / "local-runtime" / "release-checks"
        fixture_root.mkdir(parents=True, exist_ok=True)
        # Preserve local fixtures, consistent with this fork's no-delete rule.
        self.runtime = Path(tempfile.mkdtemp(dir=fixture_root))
        (self.runtime / "data").mkdir()
        self.config = self.runtime / "data/config.yaml"
        self.config.write_text('database:\n  host: localhost\n  port: 5432\n  user: fixture\n'
                               '  password: "synthetic-secret"\n  dbname: fixture\n'
                               '  sslmode: disable\nserver:\n  port: 8080\n', encoding="utf-8")

    def test_config_scoping_and_quotes(self):
        parsed = database.database_config(self.runtime)
        self.assertEqual(parsed["port"], "5432")
        self.assertEqual(parsed["password"], "synthetic-secret")
        self.config.write_text(self.config.read_text().replace('"synthetic-secret"', "'a''b'"))
        self.assertEqual(database.database_config(self.runtime)["password"], "a'b")

    def test_diagnostics_do_not_expose_password(self):
        with patch.object(database.subprocess, "run", return_value=subprocess.CompletedProcess([], 1, "", "synthetic-secret")):
            with self.assertRaisesRegex(RuntimeError, "diagnostics suppressed") as caught:
                database.execute(Path("unused"), "pg_dump.exe", [], {})
        self.assertNotIn("synthetic-secret", str(caught.exception))

    def run_backup(self, destination, execute):
        with patch.object(sys, "argv", ["database.py", "backup", "--runtime", str(self.runtime),
                                        "--pg-bin", "unused", "--directory", str(destination)]), \
             patch.object(database, "execute", side_effect=execute), contextlib.redirect_stdout(io.StringIO()) as output:
            database.main()
        return output.getvalue()

    def test_backup_and_readonly_migration_snapshot(self):
        destination = self.runtime / "backups" / "fixture"
        calls = []

        def execute(pg_bin, program, args, env):
            calls.append(program)
            self.assertEqual(env["PGPASSWORD"], "synthetic-secret")
            self.assertNotIn("synthetic-secret", args)
            if program == "pg_dump.exe":
                Path(args[-1]).write_bytes(b"fixture-archive")
                return ""
            if program == "pg_restore.exe":
                return "TABLE DATA"
            self.assertIn("default_transaction_read_only=on", env["PGOPTIONS"])
            return '["001_initial.sql"]'

        output = self.run_backup(destination, execute)
        self.assertEqual(calls, ["pg_dump.exe", "pg_restore.exe", "psql.exe"])
        self.assertEqual(json.loads(output)["migrations"], ["001_initial.sql"])
        self.assertNotIn("synthetic-secret", output)
        self.assertTrue((destination / "database-before.json").exists())
        with self.assertRaises(FileExistsError):
            self.run_backup(destination, execute)

    def test_backup_outside_runtime_rejected_before_commands(self):
        def unexpected(*args):
            self.fail("No PostgreSQL process should run")
        with self.assertRaisesRegex(ValueError, "runtime/backups"):
            self.run_backup(self.runtime / "wrong-location", unexpected)

    def test_invalid_archive_rejected(self):
        def execute(pg_bin, program, args, env):
            if program == "pg_dump.exe":
                Path(args[-1]).write_bytes(b"invalid")
                return ""
            return "invalid archive"
        with self.assertRaisesRegex(RuntimeError, "validation failed"):
            self.run_backup(self.runtime / "backups" / "invalid", execute)


if __name__ == "__main__":
    unittest.main()
