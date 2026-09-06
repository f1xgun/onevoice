# infra/seccomp — Chromium sandbox profile for the RPA worker

`chrome.json` is the seccomp profile `docker-compose.yml` attaches to
`agent-yandex-business` (`security_opt: seccomp=./infra/seccomp/chrome.json`).
It lets headless Chromium run with **its own sandbox ON** inside a container
that also has `cap_drop: [ALL]`, `no-new-privileges`, a read-only root
filesystem and a non-root user.

## Why a custom profile

Chromium's namespace sandbox creates an unprivileged user namespace
(`clone(CLONE_NEWUSER | CLONE_NEWPID | CLONE_NEWNET)`), calls `unshare` /
`setns` inside it and `chroot`s the renderer into an empty directory. Docker's
default seccomp profile gates exactly those syscalls behind `CAP_SYS_ADMIN` /
`CAP_SYS_CHROOT` or filters the `clone` flags, so with the default profile
Chromium aborts with `No usable sandbox!` — which is why the worker used to
launch with `--no-sandbox`. Granting the capabilities instead would widen the
blast radius of a renderer compromise far more than allowing six syscalls.

## What the profile is

Docker's current default profile
(`https://raw.githubusercontent.com/moby/profiles/main/seccomp/default.json`)
with one rule prepended that allows, unconditionally:

`arch_prctl`, `chroot`, `clone`, `clone3`, `setns`, `unshare`

Everything else — the default-deny action, the architecture list, every other
syscall rule — is the upstream default verbatim. Do not hand-edit the file;
regenerate it.

## Regenerate

```bash
curl -fsSL -o /tmp/default.json https://raw.githubusercontent.com/moby/profiles/main/seccomp/default.json
python3 - <<'EOF'
import json
d = json.load(open('/tmp/default.json'))
add = ["arch_prctl", "chroot", "clone", "clone3", "setns", "unshare"]
for r in d["syscalls"]:
    r["names"] = [n for n in r.get("names", []) if n not in add]
d["syscalls"] = [r for r in d["syscalls"] if r.get("names")]
d["syscalls"].insert(0, {"names": add, "action": "SCMP_ACT_ALLOW",
    "comment": "Chromium namespace sandbox: unprivileged user namespaces + chroot; the rest is Docker's default profile."})
json.dump(d, open('infra/seccomp/chrome.json', 'w'), indent=2)
EOF
```

## Validate (the launch smoke that proved the profile)

Positive — must print a DOM and no `No usable sandbox`:

```bash
docker run --rm --user pwuser --read-only \
  --tmpfs /tmp:size=256m --tmpfs /dev/shm:size=256m --tmpfs /home/pwuser:size=64m \
  --cap-drop ALL --security-opt no-new-privileges:true \
  --security-opt seccomp=$PWD/infra/seccomp/chrome.json \
  --entrypoint /ms-playwright/chromium-1155/chrome-linux/chrome \
  mcr.microsoft.com/playwright:v1.50.0-jammy \
  --headless=new --disable-gpu --no-first-run --dump-dom about:blank
```

Negative control — the same command WITHOUT the `seccomp=` option must fail
with `FATAL ... No usable sandbox!` (exit 133). If it does not, the host is
running Chromium unsandboxed for another reason and the profile is not what is
being tested.

Through compose (applies the exact service hardening):

```bash
docker compose run --rm --no-deps -T \
  --entrypoint /ms-playwright/chromium-1155/chrome-linux/chrome agent-yandex-business \
  --headless=new --disable-gpu --no-first-run --dump-dom about:blank
```

## Host requirements

The namespace sandbox needs unprivileged user namespaces on the **host**
kernel. Most distributions enable them; on kernels that gate them check
`sysctl kernel.unprivileged_userns_clone` (Debian/Ubuntu legacy knob) or
`sysctl user.max_user_namespaces` (must be > 0). Docker Desktop on macOS runs
the Linux VM with them enabled. After a production deploy run one real Yandex
RPA tool and confirm the agent log shows the browser launching without
`--no-sandbox` errors — this is the NEEDS-LIVE-VALIDATION item in
`docker-compose.yml`.

## When the Playwright image changes

The Chromium build path (`/ms-playwright/chromium-<rev>/chrome-linux/chrome`)
changes with the Playwright version pinned in `Dockerfile.agent-yandex-business`;
update the validation commands above. The syscall set is stable across
Chromium versions, but re-run the positive and negative smokes after every
bump.
