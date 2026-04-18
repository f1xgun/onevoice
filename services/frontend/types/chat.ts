// 'aborted' marks a tool_call that was persisted without a matching
// tool_result — e.g., the user refreshed mid-run and the tool was canceled
// before emitting its result.
export type ToolCallStatus = 'pending' | 'done' | 'error' | 'aborted';

export interface ToolCall {
  id: string;
  name: string;
  args: Record<string, unknown>;
  result?: Record<string, unknown>;
  error?: string;
  status: ToolCallStatus;
}

export interface Message {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  toolCalls?: ToolCall[];
  status?: 'streaming' | 'done';
}
