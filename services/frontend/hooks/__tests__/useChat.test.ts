import { describe, it, expect } from 'vitest';
import { parseSSELine, applySSEEvent } from '../useChat';
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

  it('marks done on done event', () => {
    const result = applySSEEvent(baseMessage, { type: 'done' });
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
    // Second tool finishes first — without tool_call_id correlation this
    // would update the first entry instead.
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
