# Runbook: Observability Stack Operator Access (Tailscale)

The observability stack (Grafana, Prometheus, Loki, Promtail) defined in
`docker-compose.observability.yml` does **not** bind any service to a host
port. This eliminates the public attack surface (default-credential
Grafana panels were a launch blocker — see Phase 23-02, D-09/D-10/D-11).

Operators reach Grafana over Tailscale, which:

- Authenticates each operator device against the OneVoice tailnet ACL.
- Encrypts the traffic end-to-end (WireGuard).
- Leaves zero IPv4/v6 listener on the host's public interfaces.

## Prerequisites (one-time per operator laptop)

1. Install Tailscale: <https://tailscale.com/download>
2. Authenticate against the OneVoice tailnet (auth key issued by tech lead;
   rotated quarterly).
3. Verify membership: `tailscale status` shows the production host
   (e.g. `onevoice-prod`) as reachable.

## Prerequisites (one-time per production host)

1. Install `tailscaled` on the host:

   ```bash
   curl -fsSL https://tailscale.com/install.sh | sh
   ```

2. Authenticate:

   ```bash
   sudo tailscale up --hostname=onevoice-prod --advertise-tags=tag:production
   ```

3. Verify in admin console: <https://login.tailscale.com/admin/machines>.

## ACL fragment

Add to the tailnet ACL (<https://login.tailscale.com/admin/acls>):

```jsonc
{
  "acls": [
    {
      "action": "accept",
      "src":    ["tag:operator"],
      "dst":    ["tag:production:3000", "tag:production:9090", "tag:production:3100"]
    }
  ],
  "tagOwners": {
    "tag:production": ["group:onevoice-ops"],
    "tag:operator":   ["group:onevoice-ops"]
  }
}
```

## Accessing Grafana

From an operator laptop on the tailnet:

- Browser: `http://onevoice-prod:3000`
- Username: `admin` (or value of `GF_SECURITY_ADMIN_USER`)
- Password: value of `GF_SECURITY_ADMIN_PASSWORD` from `.env.prod`
  (stored in the ops password manager; rotate quarterly).

## Accessing Prometheus / Loki directly (rare)

- Prometheus: `http://onevoice-prod:9090`
- Loki: `http://onevoice-prod:3100`

Most queries should go through Grafana datasources; direct access is
reserved for debugging the observability stack itself.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `tailscale status` shows `idle` | tailscaled not running on host | `sudo systemctl restart tailscaled` |
| Browser hangs / times out | Operator not in `tag:operator` group | Verify in <https://login.tailscale.com/admin/users> |
| `curl http://onevoice-prod:3000` returns connection refused | Grafana container not running | `docker compose -f docker-compose.observability.yml ps grafana` |
| `curl -v http://onevoice-grafana:3000` from tailnet returns DNS error | MagicDNS off, or wrong hostname | Use the host hostname (`onevoice-prod`) not the container name |
| Login fails with `admin/admin` | Password rotated since you last looked | Pull current value from password manager / `.env.prod` |

## Rotation

Rotate `GF_SECURITY_ADMIN_PASSWORD` quarterly. After rotation:

1. Update `.env.prod` on the host.
2. `docker compose -f docker-compose.observability.yml up -d grafana`
3. Verify login with new password from a fresh operator session.
4. Update password manager entry.

## References

- `docker-compose.observability.yml` — Tailnet-only obs stack
- `docs/runbook-launch-readiness.md` §7 — Grafana password launch gate
- `.env.example` §12 — `GF_SECURITY_ADMIN_PASSWORD` documentation
- Phase 23-02 plan: `.planning/phases/23-operational-hardening/23-02-grafana-auth-network-PLAN.md`
