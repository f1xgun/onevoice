// 'aborted' marks a tool_call that was persisted without a matching
// tool_result — e.g., the user refreshed mid-run and the tool was canceled
// before emitting its result.
//
// Phase 17 (HITL frontend) adds 'rejected' (user denied the call) and
// 'expired' (batch TTL elapsed before resolution). Both terminal.
export type ToolCallStatus = 'pending' | 'done' | 'error' | 'aborted' | 'rejected' | 'expired';

export interface ToolCall {
  id: string;
  name: string;
  args: Record<string, unknown>;
  result?: Record<string, unknown>;
  error?: string;
  status: ToolCallStatus;
  // Phase 17 additions (non-breaking):
  rejectReason?: string; // populated when status === 'rejected' (Phase 16 D-18)
  wasEdited?: boolean; // true when user edited args before approving (UI-SPEC §Post-submit)
}

export interface Message {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  toolCalls?: ToolCall[];
  status?: 'streaming' | 'done';
}

// ---------- Phase 17: HITL pending-approval contract ----------
//
// `GET /api/v1/conversations/{id}/messages` returns camelCase (Phase 16 backend
// serializer). The SSE `tool_approval_required` event is snake_case on the wire;
// `useChat.ts` (Plan 17-02) normalizes to camelCase at the hook boundary so the
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
// Phase 16 invariants enforced by this type (see Phase 16 D-06/D-08/D-09):
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
