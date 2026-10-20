#!/usr/bin/env python3
"""Run feedback contracts on three disposable, loopback-only databases.

Requires Docker and Go. Images must already be pulled; never touches existing
containers/databases. Credentials live only in a temporary mode-0600 env file.
"""
import json
import argparse
import os
from pathlib import Path
import secrets
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]


def run(args, **kw):
    kw.setdefault("timeout", 90)
    return subprocess.run(args, check=True, capture_output=True, text=True, **kw)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--driver", choices=["mysql", "pg", "mongo"], help="verify only one disposable backend")
    selected = parser.parse_args().driver
    names = []
    run_id = secrets.token_hex(5)
    env = os.environ.copy()
    # Never inherit a caller's unrelated database targets in single-driver mode.
    for driver in ("MYSQL", "PG", "MONGO"):
        env.pop("AGENTDECK_TEST_FEEDBACK_" + driver + "_DSN", None)
    specs = [
        ("mysql", "mysql:8.4", 3306,
         {"MYSQL_DATABASE": "agentdeck_feedback_test", "MYSQL_USER": "feedback"},
         ["sh", "-c", 'MYSQL_PWD="$MYSQL_PASSWORD" mysql -h127.0.0.1 -u"$MYSQL_USER" "$MYSQL_DATABASE" -Nse "SELECT 1"']),
        ("pg", "postgres:17", 5432,
         {"POSTGRES_DB": "agentdeck_feedback_test", "POSTGRES_USER": "feedback"},
         ["pg_isready", "-h", "127.0.0.1", "-U", "feedback", "-d", "agentdeck_feedback_test"]),
        ("mongo", "mongo:8.0", 27017,
         {"MONGO_INITDB_ROOT_USERNAME": "feedback"},
         ["mongosh", "--quiet", "--host", "127.0.0.1", "--eval", "db.adminCommand({ping:1}).ok"]),
    ]
    try:
        with tempfile.TemporaryDirectory(prefix="agentdeck-feedback-") as tmp:
            for driver, default_image, port, variables, probe in specs:
                if selected and driver != selected:
                    continue
                password = secrets.token_hex(24)
                if driver == "mysql":
                    variables.update(MYSQL_PASSWORD=password, MYSQL_ROOT_PASSWORD=secrets.token_hex(24))
                elif driver == "pg":
                    variables["POSTGRES_PASSWORD"] = password
                else:
                    variables["MONGO_INITDB_ROOT_PASSWORD"] = password
                private = Path(tmp) / (driver + ".env")
                fd = os.open(private, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
                with os.fdopen(fd, "w") as out:
                    out.write("".join(f"{k}={v}\n" for k, v in variables.items()))
                name = f"agentdeck-feedback-{run_id}-{driver}"
                image = os.getenv("FEEDBACK_TEST_" + driver.upper() + "_IMAGE", default_image)
                data_path = {"mysql": "/var/lib/mysql", "pg": "/var/lib/postgresql/data", "mongo": "/data/db"}[driver]
                names.append(name)
                run(["docker", "run", "--pull=never", "--detach", "--name", name,
                     "--label", "dev.agentdeck.feedback-test=" + run_id,
                     "--cpus", "1", "--memory", "768m", "--env-file", str(private),
                     "--tmpfs", data_path + ":rw,nosuid,size=384m",
                     "--publish", f"127.0.0.1::{port}", image])
                print(f"Started isolated {driver}; waiting for readiness", flush=True)
                for attempt in range(90):
                    result = subprocess.run(["docker", "exec", name] + probe,
                                            capture_output=True, timeout=10)
                    if result.returncode == 0:
                        break
                    if attempt % 15 == 0:
                        print(f"Waiting for {driver}", flush=True)
                    time.sleep(1)
                else:
                    raise RuntimeError(f"{driver} readiness failed")
                binding = json.loads(run(["docker", "inspect", "--format",
                    "{{json .NetworkSettings.Ports}}", name]).stdout)[f"{port}/tcp"][0]
                assert binding["HostIp"] == "127.0.0.1"
                host = "127.0.0.1:" + binding["HostPort"]
                if driver == "mysql":
                    dsn = f"feedback:{password}@tcp({host})/agentdeck_feedback_test"
                elif driver == "pg":
                    dsn = f"postgres://feedback:{password}@{host}/agentdeck_feedback_test?sslmode=disable"
                else:
                    dsn = f"mongodb://feedback:{password}@{host}/?authSource=admin"
                env["AGENTDECK_TEST_FEEDBACK_" + driver.upper() + "_DSN"] = dsn
            env["AGENTDECK_TEST_FEEDBACK_MONGO_DATABASE"] = "agentdeck_feedback_test"
            # test output deliberately excludes DSNs, credentials and report bodies.
            # The retention contract advances its clock and prunes the disposable
            # database; serialize packages so it cannot delete the HTTP fixture.
            subprocess.run(["go", "test", "-race", "-p", "1", "./internal/feedback", "./internal/gateway",
                "-run", "TestConfiguredDatabaseContract|TestFeedbackDatabaseGatewayRoundtrip",
                "-count=1", "-v"], cwd=ROOT / "backend", env=env, check=True)
    finally:
        pending = []
        for name in reversed(names):
            try:
                run(["docker", "rm", "--force", "--volumes", name], timeout=30)
            except (subprocess.CalledProcessError, subprocess.TimeoutExpired):
                pending.append(name)
        if pending:
            raise RuntimeError("Test container cleanup pending: " + ", ".join(pending))
        elif names:
            print("Removed isolated test databases and volumes", flush=True)


if __name__ == "__main__":
    try:
        main()
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired):
        # Never echo a failed command/environment containing DB credentials.
        raise SystemExit("Feedback verification failed; database/Go command did not succeed")
