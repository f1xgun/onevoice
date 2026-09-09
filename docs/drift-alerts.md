# Drift alerts

Drift detection compares the stored OneVoice profile with read-only VK and
Yandex Business snapshots. The sweep never writes profile data back to a
platform. Telegram is only the private delivery channel and is not described
as a drift source.

Each false-to-true mismatch transition creates one durable episode. Repeated
checks with the same continuing mismatch keep the episode ID, while a matching
snapshot clears it. A later mismatch starts a new episode. Current episodes
create an in-app task. Before creating a task or sending a DM, the API verifies
that the business still exists, the exact integration is active, and the
episode is still current.

Private Telegram DMs require both gates:

- DRIFT_ALERT_DELIVERY_ENABLED=true for the deployment; the default is false.
- settings.driftAlerts.enabled=true for the business; the user-facing
  preference also defaults to false.

The recipient must be a strictly positive numeric telegram_user_id from an
active Telegram integration. A disabled gate, explicit opt-out, missing
private recipient, or absent NATS connection settles the DM leg without a
send. The database field is named drift_dm_settled_at because settlement can
mean either delivered or intentionally skipped.

Transient lookup, task, dispatch, and acknowledgement errors do not settle the
episode. They increment drift_alert_retry_count and move
drift_alert_retry_at forward with bounded exponential backoff from one minute
to one hour. Pending selection only reads due episodes, so a failing first page
cannot starve later alerts.

Notification text includes the platform, translated names from a closed field
list, and an organization UUID link. It contains no organization name,
platform external ID, or raw profile value. The integrations route checks the
UUID against the signed-in user active memberships and activates that
organization before rendering route children; malformed, deleted, and
inaccessible IDs cannot switch scope.
