// Pure helpers for parsing and applying server-sent-event frames the chat
// SSE stream emits. Extracted from `hooks/useChat.ts` so both the chat
// hook and the resume-flow sibling hook can share a single implementation
// (plan 19-10).
//
// These functions are pure: no React state, no module-scoped mutation.
// Tests live alongside in `lib/__tests__/sse.test.ts`.

import type { ChatErrorCode, ErrorCode, Message, ToolCall } from '@/types/chat';

const SSE_DATA_PREFIX = 'data: ';

export function parseSSELine(line: string): Record<string, unknown> | null {
  if (!line.startsWith(SSE_DATA_PREFIX)) return null;
  try {
    return JSON.parse(line.slice(SSE_DATA_PREFIX.length));
  } catch {
    return null;
  }
}

export function applySSEEvent(msg: Message, event: Record<string, unknown>): Message {
  const type = event.type as string;

  if (type === 'text') {
    return { ...msg, content: msg.content + (event.content as string) };
  }

  if (type === 'tool_call') {
    const toolCall: ToolCall = {
      id: (event.tool_call_id as string) || crypto.randomUUID(),
      name: event.tool_name as string,
      args: (event.tool_args as Record<string, unknown>) ?? {},
      status: 'pending',
      displayNameKey: (event.tool_display_name_key as string | undefined) || undefined,
    };
    return { ...msg, toolCalls: [...(msg.toolCalls ?? []), toolCall] };
  }

  if (type === 'tool_result') {
    const callID = event.tool_call_id as string | undefined;
    const toolName = event.tool_name as string;
    const calls = msg.toolCalls ?? [];
    let matchIdx = callID ? calls.findIndex((t) => t.id === callID) : -1;
    if (matchIdx === -1) {
      matchIdx = calls.findIndex((t) => t.name === toolName && t.status === 'pending');
    }
    if (matchIdx === -1) return msg;
    const updated = calls.map((tc, i) =>
      i === matchIdx
        ? {
            ...tc,
            result: event.result as Record<string, unknown>,
            error: event.error as string | undefined,
            code: (event.code as ErrorCode | undefined) || undefined,
            status: (event.error ? 'error' : 'done') as ToolCall['status'],
          }
        : tc
    );
    return { ...msg, toolCalls: updated };
  }

  if (type === 'tool_rejected') {
    const callID = event.tool_call_id as string | undefined;
    const toolName = (event.tool_name as string) ?? '';
    const reason = (event.content as string) ?? '';
    const calls = msg.toolCalls ?? [];
    const matchIdx = callID ? calls.findIndex((t) => t.id === callID) : -1;
    if (matchIdx !== -1) {
      const updated = calls.map((tc, i) =>
        i === matchIdx
          ? {
              ...tc,
              status: 'rejected' as const,
              rejectReason: tc.rejectReason || reason,
            }
          : tc
      );
      return { ...msg, toolCalls: updated };
    }
    const synthesized: ToolCall = {
      id: callID || crypto.randomUUID(),
      name: toolName,
      args: (event.args as Record<string, unknown>) ?? {},
      status: 'rejected',
      rejectReason: reason,
    };
    return { ...msg, toolCalls: [...calls, synthesized] };
  }

  if (type === 'error') {
    // Stamp the machine-readable code + raw detail on the message instead of
    // splicing the raw Go error string into the rendered content. MessageBubble
    // localizes by code (lib/chatError.ts → chat.streamError.*) and keeps the
    // raw detail for an optional diagnostics affordance only. A missing code
    // still surfaces a generic localized line, never `[Error: ...]`.
    const code = (event.code as ChatErrorCode | undefined) || undefined;
    const detail = (event.content as string | undefined) || undefined;
    if (!code && !detail) return { ...msg, status: 'done' };
    return {
      ...msg,
      errorCode: code,
      errorDetail: detail,
      status: 'done',
    };
  }

  if (type === 'done') {
    return { ...msg, status: 'done' };
  }

  return msg;
}

// consumeSSEStream is the ONE implementation of "read a fetch Response body
// as SSE and feed parsed events to onEvent" shared by both sendMessage and
// resolveApproval (the resume path). Keeping two copies caused divergence in
// error handling and abort semantics.
export async function consumeSSEStream(
  response: Response,
  signal: AbortSignal,
  onEvent: (event: Record<string, unknown>) => void
): Promise<void> {
  if (!response.ok || !response.body) throw new Error(`HTTP ${response.status}`);
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  while (!signal.aborted) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split('\n');
    buffer = lines.pop() ?? '';
    for (const line of lines) {
      const event = parseSSELine(line.trim());
      if (event) onEvent(event);
    }
  }
  if (buffer.trim()) {
    const event = parseSSELine(buffer.trim());
    if (event) onEvent(event);
  }
}
