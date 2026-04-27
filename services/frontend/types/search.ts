/** Phase 19 / Plan 19-04 — search result type, mirrors backend service.SearchResult.
 *
 * Backend: `services/api/internal/service/search.go SearchResult` (camelCase JSON,
 * Phase 18 D-06 convention). The `marks` field carries `[start, end]` byte offsets
 * (UTF-8) into the `snippet` string — frontend wraps each range in <mark>.
 *
 * Cross-tenant scope is enforced server-side; the frontend MAY pass `project_id`
 * for project scoping but the businessID is resolved from the bearer's userID and
 * is NEVER carried in the request body.
 */
export interface SearchResult {
  conversationId: string;
  /** Empty string when this row matched only on message content, no title hit. */
  title: string;
  projectId?: string | null;
  /** Empty string when this row matched only on title, no message snippet. */
  snippet: string;
  matchCount: number;
  topMessageId?: string;
  score: number;
  /** Byte ranges in `snippet` that should be wrapped in <mark>. From backend HighlightRanges. */
  marks?: Array<[number, number]>;
  /** ISO 8601 — populated from the conversation's last_message_at. */
  lastMessageAt?: string | null;
}
