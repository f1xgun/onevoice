export type ToolCallStatus = 'pending' | 'done' | 'error'

export interface ToolCall {
  name: string
  args: Record<string, unknown>
  result?: Record<string, unknown>
  error?: string
  status: ToolCallStatus
}

export interface Message {
  id: string
  role: 'user' | 'assistant'
  content: string
  toolCalls?: ToolCall[]
  status?: 'streaming' | 'done'
}
