#!/usr/bin/env python3
"""Validate deployable configs using isolated vendor parsers and fake credentials."""

import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent
NGINX = "nginx@sha256:1d13701a5f9f3fb01aaa88cef2344d65b6b5bf6b7d9fa4cf0dca557a8d7702ba"


def run(*args, **kwargs):
    return subprocess.run(args, cwd=ROOT, check=True, **kwargs)


def main():
    compose_files = sorted(ROOT.glob("docker-compose*.yml"))
    env = os.environ.copy()
    for path in compose_files:
        for key in re.findall(r"\$\{([A-Z][A-Z0-9_]*)", path.read_text()):
            env.pop(key, None)
    for path in compose_files:
        for key in re.findall(r"\$\{([A-Z][A-Z0-9_]*):?\?", path.read_text()):
            env[key] = "validation-only"
    env.update(DOMAIN="example.test", ACME_EMAIL="ops@example.test",
               APP_ENV="production", PUBLIC_URL="https://example.test")
    combinations = [[], ["docker-compose.override.yml"],
                    ["docker-compose.prod.yml"], ["docker-compose.observability.yml"],
                    ["docker-compose.prod.yml", "docker-compose.observability.yml"]]
    for overlays in combinations:
        args = ["docker", "compose", "--env-file", "/dev/null", "-f", "docker-compose.yml"]
        for overlay in overlays:
            args.extend(["-f", overlay])
        result = run(*args, "config", "--format", "json", env=env, capture_output=True, text=True)
        config = json.loads(result.stdout)
        for name, service in config["services"].items():
            assert service["deploy"]["resources"]["limits"]["memory"], name
            assert service["logging"]["options"] == {"max-size": "50m", "max-file": "5"}, name
        print("Compose validated:", ", ".join(["base"] + overlays), flush=True)
    for path in (ROOT / "infra").rglob("*.json"):
        json.loads(path.read_text())
    for path in sorted((ROOT / "scripts").rglob("*.sh")):
        run("bash", "-n", str(path))
    with tempfile.TemporaryDirectory(prefix=".infra-check-", dir=ROOT) as directory:
        temp = Path(directory)
        run("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
            "-subj", "/CN=example.test", "-keyout", str(temp / "privkey.pem"),
            "-out", str(temp / "fullchain.pem"), stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for source in ["nginx.conf", "nginx.conf.template"]:
            rendered = (ROOT / "nginx" / source).read_text().replace("${DOMAIN}", "example.test")
            (temp / "nginx.conf").write_text(rendered)
            run("docker", "run", "--rm", "--network", "none",
                "-v", f"{temp}/nginx.conf:/etc/nginx/nginx.conf:ro",
                "-v", f"{temp}:/etc/letsencrypt/live/example.test:ro",
                "--entrypoint", "nginx", NGINX, "-t")
            fixture = rendered.replace("http://api:8080", "http://127.0.0.1:18080").replace(
                "http://frontend:3000", "http://127.0.0.1:18080")
            fixture = fixture.replace("http {", """http {
  server {
    listen 18080;
    client_max_body_size 20m;
    location / { add_header X-Powered-By Next.js; return 200 'frontend'; }
    location /api/v1/ { return 401 '{"error":"unauthorized"}'; }
    location = /health/live { return 200 '{"status":"alive"}'; }
    location = /health/ready { return 200 '{"checks":{"postgres":"private"}}'; }
  }
""", 1)
            (temp / "nginx.conf").write_text(fixture)
            origin = "https://127.0.0.1" if source.endswith("template") else "http://127.0.0.1"
            run("docker", "run", "--rm", "--network", "none",
                "-v", f"{temp}/nginx.conf:/etc/nginx/nginx.conf:ro",
                "-v", f"{temp}:/etc/letsencrypt/live/example.test:ro",
                "-e", f"ORIGIN={origin}", "--entrypoint", "sh", NGINX, "-ec", """
nginx
status=$(curl -ksS --http1.1 --max-time 10 -D /tmp/headers -o /tmp/body -w '%{http_code}' "$ORIGIN/")
test "$status" = 200
for header in X-Content-Type-Options X-Frame-Options Referrer-Policy Permissions-Policy Content-Security-Policy; do
  grep -qi "$header:" /tmp/headers
done
if grep -Ei 'Server: nginx/|X-Powered-By:' /tmp/headers; then exit 1; fi
curl -fksS --max-time 10 -o /tmp/body "$ORIGIN/health/live"
grep -q alive /tmp/body
status=$(curl -ksS --max-time 10 -o /tmp/body -w '%{http_code}' "$ORIGIN/health/ready")
test "$status" = 404
if grep -q private /tmp/body; then exit 1; fi
dd if=/dev/zero of=/tmp/upload bs=1048576 count=2 2>/dev/null
status=$(curl -ksS --max-time 10 -o /tmp/body -w '%{http_code}' --data-binary @/tmp/upload "$ORIGIN/api/v1/businesses/example/logo")
test "$status" = 401
grep -q unauthorized /tmp/body
dd if=/dev/zero of=/tmp/upload bs=1048576 count=11 2>/dev/null
status=$(curl -ksS --max-time 10 -o /tmp/body -w '%{http_code}' --data-binary @/tmp/upload "$ORIGIN/api/v1/businesses/example/logo")
test "$status" = 413
""")
            print("Edge behavior validated:", source, flush=True)
        (temp / "auth.conf").write_text('authorization { users: [] }\n')
        run("docker", "run", "--rm", "--network", "none",
            "-v", f"{ROOT}/infra/nats/nats-server.conf:/etc/nats/nats-server.conf:ro",
            "-v", f"{temp}/auth.conf:/etc/nats/creds/auth.conf:ro",
            "-v", f"{temp}/fullchain.pem:/etc/nats/certs/nats.crt:ro",
            "-v", f"{temp}/privkey.pem:/etc/nats/certs/nats.key:ro",
            "nats:2.10-alpine", "-t", "-c", "/etc/nats/nats-server.conf")
    run("docker", "run", "--rm", "--network", "none",
        "-v", f"{ROOT}/observability/loki/loki-config.yml:/etc/loki/config.yml:ro",
        "grafana/loki:3.0.0", "-config.file=/etc/loki/config.yml", "-verify-config=true")
    run("docker", "run", "--rm", "--network", "none",
        "-v", f"{ROOT}/observability/prometheus:/etc/prometheus:ro",
        "--entrypoint", "/bin/promtool", "prom/prometheus:v2.52.0",
        "check", "config", "/etc/prometheus/prometheus.yml")
    print("Infrastructure validation passed", flush=True)


if __name__ == "__main__":
    main()
