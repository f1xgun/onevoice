// 'aborted' marks a tool_call that was persisted without a matching
// tool_result — e.g., the user refreshed mid-run and the tool was canceled
// before emitting its result.
//
// HITL frontend adds 'rejected' (user denied the call) and
// 'expired' (batch TTL elapsed before resolution). Both terminal.
export type ToolCallStatus = 'pending' | 'done' | 'error' | 'aborted' | 'rejected' | 'expired';

/**
 * Locked enum of typed error classifiers stamped by the platform agent
 * classifier (pkg/a2a.CodedError → ToolResponse.Code → SSE tool_result.code).
 * The set is closed — FE renders by direct switch; unknown values fall through
 * to the calm fallback summary.
 */
export type ErrorCode =
  | 'integration_token_invalid'
  | 'rate_limit_exceeded'
  | 'transient'
  | 'channel_not_found'
  | 'media_too_large';

export interface ToolCall {
  id: string;
  name: string;
  args: Record<string, unknown>;
  result?: Record<string, unknown>;
  error?: string;
  /** Typed classifier carried alongside `error` on tool_result frames. */
  code?: ErrorCode;
  status: ToolCallStatus;
  // HITL additions (non-breaking):
  rejectReason?: string; // populated when status === 'rejected'
  wasEdited?: boolean; // true when user edited args before approving (UI-SPEC §Post-submit)
  // Optional i18n catalog key; FE falls back to the tool name if absent.
  displayNameKey?: string;
}

export interface Message {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  toolCalls?: ToolCall[];
  status?: 'streaming' | 'done';
}

// ---------- HITL pending-approval contract ----------
//
// `GET /api/v1/conversations/{id}/messages` returns camelCase (backend
// serializer). The SSE `tool_approval_required` event is snake_case on the wire;
// `useChat.ts` normalizes to camelCase at the hook boundary so the
// rest of the frontend only ever sees the shape below.

export interface PendingApprovalCall {
  callId: string;
  toolName: string;
  args: Record<string, unknown>;
  editableFields: string[];
  floor: string; // 'manual' in v1.3; 'forbidden' should never reach frontend.
}

export interface PendingApproval {
  batchId: string;
  conversationId?: string; // present on hydration path; absent on SSE arrival.
  status: 'pending' | 'expired';
  calls: PendingApprovalCall[];
  expiresAt?: string; // ISO — present on hydration; synthesized on SSE arrival.
  createdAt: string; // ISO
}

export type ApprovalAction = 'approve' | 'edit' | 'reject';

// Body entry sent to
// `POST /api/v1/conversations/{id}/pending-tool-calls/{batch_id}/resolve`.
//
// Invariants enforced by this type:
//   - The server-pinned toolName field is NEVER included in the resolve body —
//     the backend reads it from the persisted batch. Sending it signals a
//     contract misunderstanding.
//   - `edited_args` is present ONLY when `action === 'edit'`, and may contain
//     ONLY top-level scalar changes the user actually made.
//   - `reject_reason` is clamped to 500 chars client-side before submit.
export interface ApprovalDecision {
  id: string; // matches PendingApprovalCall.callId
  action: ApprovalAction;
  edited_args?: Record<string, string | number | boolean>;
  reject_reason?: string;
}
