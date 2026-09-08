# Guided compose

`GuidedCompose` is a frontend form that seeds an instruction into the existing
conversation `sendMessage` path. It does not create drafts, approvals, or tool
tasks itself. The normal chat SSE and HITL flow remains responsible for model
selection, metering, redaction, per-channel editing, approval, and dispatch.

The channel picker derives its options from two successful API reads:

- the platform registry must mark the platform `active`;
- the current business integration data must resolve to `connected`.

Google Business, unavailable registry entries, and pending, broken, malformed,
or failed integration evidence are excluded. The form starts with all confirmed
channels selected and lets the operator narrow the set. Its seeded instruction
names the selected platform IDs and explicitly limits publication to them, which
overrides the general broadcast-all prompt directive without changing the
cache-locked platform prompt block.

Changing the active business clears the topic and channel selection. Fresh
successful evidence initializes selections for the new business; a channel
removed by later integration data is also removed from the form before submit.
