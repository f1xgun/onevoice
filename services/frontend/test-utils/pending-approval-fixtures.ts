import type { PendingApproval } from '@/types/chat';

// Canonical Phase-17 PendingApproval fixtures. Shared across
// useChat / ToolApprovalCard / ChatWindow tests so we never re-hand-roll
// a batch shape per test (diverges silently and poisons assertions).

export const singleCallBatch: PendingApproval = {
  batchId: 'batch-single',
  status: 'pending',
  createdAt: '2026-04-24T07:00:00Z',
  expiresAt: '2026-04-25T07:00:00Z',
  calls: [
    {
      callId: 'call-single-1',
      toolName: 'telegram__send_channel_post',
      args: { chat_id: 123, text: 'hello' },
      editableFields: ['text', 'parse_mode'],
      floor: 'manual',
    },
  ],
};

export const threeCallBatch: PendingApproval = {
  batchId: 'batch-three',
  status: 'pending',
  createdAt: '2026-04-24T07:00:00Z',
  expiresAt: '2026-04-25T07:00:00Z',
  calls: [
    {
      callId: 'c1',
      toolName: 'telegram__send_channel_post',
      args: { chat_id: 1, text: 'A' },
      editableFields: ['text', 'parse_mode'],
      floor: 'manual',
    },
    {
      callId: 'c2',
      toolName: 'vk__create_post',
      args: { owner_id: -1, message: 'B' },
      editableFields: ['message'],
      floor: 'manual',
    },
    {
      callId: 'c3',
      toolName: 'yandex_business__reply_review',
      args: { review_id: 'r', text: 'C' },
      editableFields: ['text'],
      floor: 'manual',
    },
  ],
};

export const nestedArgsBatch: PendingApproval = {
  batchId: 'batch-nested',
  status: 'pending',
  createdAt: '2026-04-24T07:00:00Z',
  expiresAt: '2026-04-25T07:00:00Z',
  calls: [
    {
      callId: 'c-nested',
      toolName: 'tool_with_nested_args',
      args: { text: 'top', meta: { text: 'nested', author: 'alice' } },
      editableFields: ['text'], // 'text' inside meta.* MUST still be rejected by onEdit.
      floor: 'manual',
    },
  ],
};

export const noEditableFieldsBatch: PendingApproval = {
  batchId: 'batch-no-edits',
  status: 'pending',
  createdAt: '2026-04-24T07:00:00Z',
  expiresAt: '2026-04-25T07:00:00Z',
  calls: [
    {
      callId: 'c-readonly',
      toolName: 'telegram__delete_message',
      args: { chat_id: 1, message_id: 2 },
      editableFields: [],
      floor: 'manual',
    },
  ],
};

export const expiredBatch: PendingApproval = {
  ...singleCallBatch,
  batchId: 'batch-expired',
  status: 'expired',
};
