# Frontend configuration

`LANDING_ENTRY_MODE` is a server-only runtime setting. The landing route uses
`force-dynamic` and reads the environment on each server render. It is deliberately
absent from Next.js build-time `env`, Docker build args and `NEXT_PUBLIC_*`.

| Value | Entry points |
| --- | --- |
| `waitlist_only` | Waitlist primary; no registration links |
| `hybrid` (default, also for invalid values) | Waitlist primary; registration secondary |
| `open` | Free registration primary; waitlist link in the Pro beta-price hook |

Both Compose configurations pass the setting to the frontend at runtime. No image
rebuild is needed. A process restart picks up a changed process environment.
Changing `.env` alone followed by `docker compose restart` does **not** update an
existing container's environment: the deployment operator must recreate only the
frontend container with the new environment and the existing image. Verify the
rendered links after the rollout. Do not restart the API or database.

Landing CTA telemetry uses the public `/api/v1/landing-events` endpoint without
an authenticated API client. Navigation never waits for telemetry delivery.
