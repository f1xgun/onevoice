# Post-action task verification

Successful supported platform writes freeze their exact serialized fields and external target on the `AgentTask` before dispatch. After the write reaches `done`, a startup-owned bounded worker reads the same object back and records a separate verification state. Missing fields are `unverifiable`; only a complete exact match is `verified`. Write status and output are never replaced by readback results.

Telegram reads channel title and description. VK reads group title, description, and website; a written phone remains intended but unreadable, so the complete action is unverifiable. Yandex `update_info` uses the existing `get_info` RPA tool with a 90-second bound and preserves response-field presence. Yandex hours are read within the same bound but remain unverifiable because the rendered value cannot prove equality with the serialized schedule. Photos, posts, and replies are unsupported.

`POST /api/v1/businesses/{id}/tasks/{taskId}/rerun` repeats only readback. It requires business access and `content:read`, is write-rate-limited because it changes internal task metadata, and rejects deleted businesses. It never calls the failed-write retry dispatcher.

Verification jobs use compare-and-set attempt numbers. Queue rejection and interrupted process leftovers become recoverable `error` results, and completion persistence uses a short context detached from request/process cancellation. SSE reuses `task.updated`; frozen expected values and targets are excluded from JSON.
