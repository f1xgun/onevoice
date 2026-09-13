# Frontend configuration

`LANDING_ENTRY_MODE` is a server-only runtime setting. The landing route uses
`force-dynamic` and reads the environment on each server render. It is deliberately
absent from Next.js build-time `env`, Docker build args and `NEXT_PUBLIC_*`.

| Value                                       | Entry points                                                        |
| ------------------------------------------- | ------------------------------------------------------------------- |
| `waitlist_only`                             | Waitlist primary; no registration links                             |
| `hybrid` (default, also for invalid values) | Waitlist primary; registration secondary                            |
| `open`                                      | Free registration primary; waitlist link in the Pro beta-price hook |

`REGISTRATION_MODE` is the account-creation authority shared with the API:

| Value         | Frontend behavior                                                                                                           | API behavior                                                                                                         |
| ------------- | --------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `invite_only` | Forces the landing to `waitlist_only`. Direct `/register` explains the closed beta; `/register?invited=1` renders the form. | Accepts registration only when the normalized email has a consented `waitlist_signups` row with `access_granted_at`. |
| `open`        | Uses `LANDING_ENTRY_MODE` as configured.                                                                                    | Accepts any valid registration.                                                                                      |

The invitation query flag contains no credential and only reveals the form. The
API email grant is authoritative, so sharing the URL cannot bypass the cohort.
Both the base and production Compose files default to `invite_only`, as does
`.env.example`. This also keeps a base-only deployment with `APP_ENV=production`
closed. Set `REGISTRATION_MODE=open` explicitly for unrestricted local signup;
an explicit `open` overrides the production default too. An unknown explicit
frontend value fails closed, while the API refuses to start with it.

For processes launched without Compose, set `REGISTRATION_MODE` explicitly and
identically for API and frontend. The standalone API defaults to `invite_only`
when `APP_ENV=production`, otherwise `open`; the standalone frontend defaults to
`open` when the setting is absent. Do not rely on those different process defaults
for a cohort rollout.

Both Compose configurations pass the setting to the frontend at runtime. No image
rebuild is needed. A process restart picks up a changed process environment.
Changing `.env` alone followed by `docker compose restart` does **not** update an
existing container's environment. Changing `LANDING_ENTRY_MODE` needs frontend
recreation; changing source copy still needs the normal image build/deploy.
A `REGISTRATION_MODE` change must recreate both API and frontend with the same
value and existing images. Verify the rendered links and a rejected
unapproved registration after rollout.

For a production Compose deployment, first set the mode in the deployment
environment file. Use the same env file and Compose layers that created the
running project (the example uses `.env`):

```sh
docker compose --env-file .env -f docker-compose.yml -f docker-compose.prod.yml \
  up -d --no-deps --no-build --force-recreate api frontend
for service in api frontend; do
  docker compose --env-file .env -f docker-compose.yml -f docker-compose.prod.yml \
    exec -T "$service" sh -c 'printf "%s\n" "$REGISTRATION_MODE"'
done
```

Both lines must show the intended identical mode. During a switch to
`invite_only`, restrict public ingress until both services have been recreated
and the API rejection check passes; recreation is not an atomic switch across
services. Existing login remains available after the rollout. Apply the access
grant migration before deploying this API version or issuing grants. See the
[operator workflow](runbook-founder-manual-actions.md#гибридный-вход-на-лендинге).

Landing CTA telemetry uses the public `/api/v1/landing-events` endpoint without
an authenticated API client. Navigation never waits for telemetry delivery.
