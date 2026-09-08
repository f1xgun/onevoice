# Requests for future channels

The integrations page records interest in Avito, 2GIS, Wildberries, and Ozon through
the existing `POST /businesses/{id}/channel-requests` endpoint. These cards do not
connect a channel or promise a delivery date. Google Business and WhatsApp remain
unavailable and have no request action because the demand API does not accept
their identifiers.

The page first reads `GET /businesses/{id}/channel-requests`. A positive saved count
renders a confirmed, disabled action, including after reload. Pending requests are
guarded against repeated clicks. Failed reads offer retry and block submission;
failed writes remain retryable and never show a saved confirmation. The cache and
mutation variables carry the organization ID, so switching organizations cannot
transfer another organization's confirmation.

Reads require `content.read`; writes require `content.create`, matching the API.
Permission lookup failures have a retry action. The server still enforces tenant
access, permissions, the channel allowlist, and the existing per-user write limit.
The UI prevents duplicate clicks in one mounted session; the existing API remains
an append-only demand ledger and does not deduplicate concurrent tabs or clients.

After a successful write, the frontend emits `activation / waitlist_platform`
with the channel in `platform` and the captured organization ID in `business_id`.
This best-effort telemetry event is separate from the durable, server-scoped
`channel_demand_signals` record. No event is emitted on a failed write or merely
loading an existing request. Reload confirmation relies on the durable demand
record, so losing the optional telemetry buffer does not lose the request.

The feature adds no integration provider, background notification, or outbound
message. It preserves the existing channel registry and approval requirements.
