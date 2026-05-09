// Tool-approval flow constants shared between the parent ToolApprovalCard
// reducer (which truncates) and ToolApprovalAccordionEntry (which surfaces
// the inline length counter). Sharing the value here avoids drift if the
// limit changes — the counter must stop flagging "over" at exactly the
// position the reducer would slice.

export const REJECT_REASON_MAX_LEN = 500;
