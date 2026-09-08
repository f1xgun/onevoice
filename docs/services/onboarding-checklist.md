# Organization setup checklist

The checklist derives its progress from existing organization, integration, profile,
and membership queries. Connecting any one channel completes the shared connection
step; additional platform rows are optional and do not increase the required total.
The first-action step uses the server's `hasFirstSuccessfulAction` flag.

Platform rows require a successful registry response and show only available
platforms. Unconfigured OAuth, future platforms, and the unverified Google flow are
excluded. Integration queries are keyed by organization. Loading, unavailable, and
malformed responses never imply a confirmed disconnection. Explicit connection
states reuse `channelConnectionState`.

Each row opens the existing connection flow through `/integrations?connect=<platform>`
or the reconnect flow when the API reports a broken connection. The connection
entry point checks permission and registry availability. Successful connection
invalidation refreshes the checklist without a page reload.

Checklist actions emit `activation` / `activation_step` with `business_id`,
`platform`, and `step`. These events measure clicks on setup actions, not completed
connections or successful external actions. General checklist actions use an empty
platform value. Telemetry is optional; authoritative completion remains derived
from saved API state. The checklist emits no organization descriptions or content.
