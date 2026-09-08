# Review delegation metrics

`GET /businesses/{id}/delegation-metrics` reports eight Monday-to-Monday UTC calendar weeks, including the current partial week. The repository matches `replied_at` in the half-open interval `[from, to)` and groups in MongoDB, so it returns bounded aggregate rows without loading author names, review text, drafts, or final replies.

`acceptedUnedited` and `edited` count only replies with an exact saved `DraftAcceptedUnedited` signal. Their sum is the `measurable` denominator. A timestamped reply with no signal counts as `unknown`; a legacy reply without `replied_at` cannot be assigned to a week and is excluded from every weekly and summary total. Historical edits are never inferred.

Few-shot selection uses two bounded indexed reads. Half of the candidate pool is reserved for confirmed operator-edited replies and the remainder for recent accepted or legacy replies. The final selector interleaves those groups and removes near-duplicate reply openings, preserving the existing business/platform filters, recency mix, and bounded pool cap.
