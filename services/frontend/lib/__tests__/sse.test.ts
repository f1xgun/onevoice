import { describe, it, expect } from 'vitest';
import { parseSSELine, applySSEEvent } from '../sse';
import type { Message } from '@/types/chat';

describe('parseSSELine', () => {
  it('returns null for non-data lines', () => {
    expect(parseSSELine('')).toBeNull();
    expect(parseSSELine(': keep-alive')).toBeNull();
  });

  it('parses data line to object', () => {
    const result = parseSSELine('data: {"type":"text","content":"hello"}');
    expect(result).toEqual({ type: 'text', content: 'hello' });
  });

  it('returns null for malformed JSON', () => {
    expect(parseSSELine('data: {bad json}')).toBeNull();
  });
});

describe('applySSEEvent', () => {
  const baseMessage: Message = {
    id: '1',
    role: 'assistant',
    content: '',
    toolCalls: [],
    status: 'streaming',
  };

  it('appends text to content', () => {
    const result = applySSEEvent(baseMessage, { type: 'text', content: ' world' });
    expect(result.content).toBe(' world');
  });

  it('adds tool_call entry as pending', () => {
    const result = applySSEEvent(baseMessage, {
      type: 'tool_call',
      tool_name: 'vk__publish_post',
      tool_args: { text: 'hello' },
    });
    expect(result.toolCalls).toHaveLength(1);
    expect(result.toolCalls![0].status).toBe('pending');
    expect(result.toolCalls![0].name).toBe('vk__publish_post');
  });

  it('updates tool_call to done on tool_result', () => {
    const msg: Message = {
      ...baseMessage,
      toolCalls: [{ id: '', name: 'vk__publish_post', args: {}, status: 'pending' }],
    };
    const result = applySSEEvent(msg, {
      type: 'tool_result',
      tool_name: 'vk__publish_post',
      result: { post_id: '123' },
    });
    expect(result.toolCalls![0].status).toBe('done');
    expect(result.toolCalls![0].result).toEqual({ post_id: '123' });
  });

  it('propagates typed code on tool_result error frames', () => {
    const msg: Message = {
      ...baseMessage,
      toolCalls: [
        { id: 'call_1', name: 'telegram__send_channel_post', args: {}, status: 'pending' },
      ],
    };
    const result = applySSEEvent(msg, {
      type: 'tool_result',
      tool_call_id: 'call_1',
      tool_name: 'telegram__send_channel_post',
      error: 'Unauthorized: bot kicked',
      code: 'integration_token_invalid',
    });
    expect(result.toolCalls![0].status).toBe('error');
    expect(result.toolCalls![0].error).toBe('Unauthorized: bot kicked');
    expect(result.toolCalls![0].code).toBe('integration_token_invalid');
  });

  it('leaves code undefined when tool_result frame omits it (legacy)', () => {
    const msg: Message = {
      ...baseMessage,
      toolCalls: [
        { id: 'call_1', name: 'telegram__send_channel_post', args: {}, status: 'pending' },
      ],
    };
    const result = applySSEEvent(msg, {
      type: 'tool_result',
      tool_call_id: 'call_1',
      tool_name: 'telegram__send_channel_post',
      result: { message_id: 7 },
    });
    expect(result.toolCalls![0].status).toBe('done');
    expect(result.toolCalls![0].code).toBeUndefined();
  });

  it('marks done on done event', () => {
    const result = applySSEEvent(baseMessage, { type: 'done' });
    expect(result.status).toBe('done');
  });

  it('stamps errorCode + errorDetail instead of splicing a raw [Error: ...] string', () => {
    const result = applySSEEvent(baseMessage, {
      type: 'error',
      code: 'max_iterations',
      content: 'max iterations (10) reached',
    });
    expect(result.content).toBe('');
    expect(result.errorCode).toBe('max_iterations');
    expect(result.errorDetail).toBe('max iterations (10) reached');
    expect(result.status).toBe('done');
  });

  it('preserves existing assistant content and never appends the raw error', () => {
    const withText: Message = { ...baseMessage, content: 'Привет.' };
    const result = applySSEEvent(withText, {
      type: 'error',
      code: 'internal_error',
      content: 'openrouter 429',
    });
    expect(result.content).toBe('Привет.');
    expect(result.content).not.toContain('[Error:');
    expect(result.errorCode).toBe('internal_error');
    expect(result.errorDetail).toBe('openrouter 429');
    expect(result.status).toBe('done');
  });

  it('records detail with no code when the frame omits code (unknown → FE fallback)', () => {
    const result = applySSEEvent(baseMessage, { type: 'error', content: 'something raw' });
    expect(result.errorCode).toBeUndefined();
    expect(result.errorDetail).toBe('something raw');
    expect(result.status).toBe('done');
  });

  it('marks done on error event even when content and code are empty', () => {
    const result = applySSEEvent(baseMessage, { type: 'error', content: '' });
    expect(result.content).toBe('');
    expect(result.errorCode).toBeUndefined();
    expect(result.errorDetail).toBeUndefined();
    expect(result.status).toBe('done');
  });

  it('uses tool_call_id when provided', () => {
    const msg = applySSEEvent(baseMessage, {
      type: 'tool_call',
      tool_call_id: 'call_42',
      tool_name: 'vk__publish_post',
      tool_args: {},
    });
    expect(msg.toolCalls![0].id).toBe('call_42');
  });

  it('flips an existing tool_call to rejected on tool_rejected event', () => {
    const msg: Message = {
      ...baseMessage,
      toolCalls: [
        {
          id: 'call_x',
          name: 'telegram__send_channel_post',
          args: { text: 'hi' },
          status: 'pending',
        },
      ],
    };
    const result = applySSEEvent(msg, {
      type: 'tool_rejected',
      tool_call_id: 'call_x',
      tool_name: 'telegram__send_channel_post',
      content: 'user said no',
    });
    expect(result.toolCalls![0].status).toBe('rejected');
    expect(result.toolCalls![0].rejectReason).toBe('user said no');
    expect(result.toolCalls![0].args).toEqual({ text: 'hi' });
  });

  it('preserves a pre-populated rejectReason rather than overwriting with the server reason', () => {
    const msg: Message = {
      ...baseMessage,
      toolCalls: [
        {
          id: 'call_x',
          name: 'telegram__send_channel_post',
          args: {},
          status: 'rejected',
          rejectReason: 'Слишком резко',
        },
      ],
    };
    const result = applySSEEvent(msg, {
      type: 'tool_rejected',
      tool_call_id: 'call_x',
      tool_name: 'telegram__send_channel_post',
      content: 'user_rejected',
    });
    expect(result.toolCalls![0].rejectReason).toBe('Слишком резко');
  });

  it('synthesizes a rejected tool_call when no entry exists (TOCTOU policy_revoked path)', () => {
    const result = applySSEEvent(baseMessage, {
      type: 'tool_rejected',
      tool_call_id: 'orphan_call',
      tool_name: 'vk__publish_post',
      content: 'policy_revoked',
    });
    expect(result.toolCalls).toHaveLength(1);
    expect(result.toolCalls![0].id).toBe('orphan_call');
    expect(result.toolCalls![0].name).toBe('vk__publish_post');
    expect(result.toolCalls![0].status).toBe('rejected');
    expect(result.toolCalls![0].rejectReason).toBe('policy_revoked');
    expect(result.toolCalls![0].args).toEqual({});
  });

  it('synthesized rejection carries args when provided (usePendingApprovalFlow projection)', () => {
    const result = applySSEEvent(baseMessage, {
      type: 'tool_rejected',
      tool_call_id: 'call_p',
      tool_name: 'telegram__send_channel_post',
      content: 'no thanks',
      args: { text: 'preview', chat_id: 1 },
    });
    expect(result.toolCalls![0].args).toEqual({ text: 'preview', chat_id: 1 });
  });

  it('correlates duplicate tool names by tool_call_id', () => {
    let msg = applySSEEvent(baseMessage, {
      type: 'tool_call',
      tool_call_id: 'call_a',
      tool_name: 'telegram__send_channel_post',
      tool_args: { text: 'first' },
    });
    msg = applySSEEvent(msg, {
      type: 'tool_call',
      tool_call_id: 'call_b',
      tool_name: 'telegram__send_channel_post',
      tool_args: { text: 'second' },
    });
    msg = applySSEEvent(msg, {
      type: 'tool_result',
      tool_call_id: 'call_b',
      tool_name: 'telegram__send_channel_post',
      result: { message_id: 2 },
    });

    expect(msg.toolCalls![0].status).toBe('pending');
    expect(msg.toolCalls![1].status).toBe('done');
    expect(msg.toolCalls![1].result).toEqual({ message_id: 2 });
  });
});
