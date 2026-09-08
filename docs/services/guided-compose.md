# Guided compose

`GuidedCompose` is a frontend form that seeds an instruction into the existing
conversation `sendMessage` path. It does not create drafts, approvals, or tool
tasks itself. The normal chat SSE and HITL flow remains responsible for model
selection, metering, redaction, per-channel editing, approval, and dispatch.

The channel picker derives its options from two successful API reads:

- the platform registry must mark the platform `active`;
- the current business integration data must resolve to `connected`.

Google Business, unavailable registry entries, and pending, broken, malformed,
or failed integration evidence are excluded. A platform remains eligible when
at least one of its integration rows is independently connected. Operators who
lack `integrations.read` keep the ordinary chat composer, while the shortcut
explains that it cannot verify destinations without connection visibility.
Failed registry or integration reads expose an explicit retry action.

The form starts with all confirmed channels selected and lets the operator
narrow the set. Its localized seed names the selected platform IDs. The same IDs
travel as structured `selected_platforms` data through the API and orchestrator.
The server accepts only the closed set `telegram`, `vk`, and
`yandex_business`, deduplicates the list, bounds it to three entries, and
intersects it with fresh active integrations. Omission preserves ordinary chat;
an explicitly empty or invalid list is rejected.

The orchestrator offers platform tools only from that intersection and stores
the scope with every approval batch. Approval and resume reapply it against
fresh integration state, including after another pause. This guarantees that
out-of-scope publication tool calls cannot reach dispatch. The natural-language
draft remains model output and is not itself a policy boundary. The cached
platform prompt block, manual approval floors, metering, and redaction are
unchanged.

Changing the active business clears the topic and channel selection. Fresh
successful evidence initializes selections for the new business; a channel
removed by later integration data is also removed from the form before submit.
