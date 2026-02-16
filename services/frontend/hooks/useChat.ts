import { useState, useCallback, useRef } from 'react'
import { useAuthStore } from '@/lib/auth'
import type { Message, ToolCall } from '@/types/chat'

// Exported for unit testing
export function parseSSELine(line: string): Record<string, unknown> | null {
  if (!line.startsWith('data: ')) return null
  try {
    return JSON.parse(line.slice(6))
  } catch {
    return null
  }
}

export function applySSEEvent(
  msg: Message,
  event: Record<string, unknown>
): Message {
  const type = event.type as string

  if (type === 'text') {
    return { ...msg, content: msg.content + (event.content as string) }
  }

  if (type === 'tool_call') {
    const toolCall: ToolCall = {
      name: event.tool_name as string,
      args: (event.tool_args as Record<string, unknown>) ?? {},
      status: 'pending',
    }
    return { ...msg, toolCalls: [...(msg.toolCalls ?? []), toolCall] }
  }

  if (type === 'tool_result') {
    const toolName = event.tool_name as string
    const updated = (msg.toolCalls ?? []).map((tc) =>
      tc.name === toolName
        ? {
            ...tc,
            result: event.result as Record<string, unknown>,
            error: event.error as string | undefined,
            status: (event.error ? 'error' : 'done') as ToolCall['status'],
          }
        : tc
    )
    return { ...msg, toolCalls: updated }
  }

  if (type === 'done') {
    return { ...msg, status: 'done' }
  }

  return msg
}

export function useChat(conversationId: string) {
  const [messages, setMessages] = useState<Message[]>([])
  const [isStreaming, setIsStreaming] = useState(false)
  const accessToken = useAuthStore((s) => s.accessToken)
  const abortRef = useRef<AbortController | null>(null)

  const sendMessage = useCallback(
    async (text: string) => {
      if (isStreaming) return

      const userMessage: Message = {
        id: crypto.randomUUID(),
        role: 'user',
        content: text,
        status: 'done',
      }

      const assistantMessage: Message = {
        id: crypto.randomUUID(),
        role: 'assistant',
        content: '',
        toolCalls: [],
        status: 'streaming',
      }

      setMessages((prev) => [...prev, userMessage, assistantMessage])
      setIsStreaming(true)

      const controller = new AbortController()
      abortRef.current = controller

      try {
        const response = await fetch(`/chat/${conversationId}`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: `Bearer ${accessToken}`,
          },
          body: JSON.stringify({ message: text }),
          signal: controller.signal,
        })

        const reader = response.body!.getReader()
        const decoder = new TextDecoder()
        let buffer = ''

        while (true) {
          const { done, value } = await reader.read()
          if (done) break

          buffer += decoder.decode(value, { stream: true })
          const lines = buffer.split('\n')
          buffer = lines.pop() ?? ''

          for (const line of lines) {
            const event = parseSSELine(line.trim())
            if (!event) continue
            setMessages((prev) => {
              const last = prev[prev.length - 1]
              if (last.role !== 'assistant') return prev
              return [...prev.slice(0, -1), applySSEEvent(last, event)]
            })
          }
        }
      } catch (error: unknown) {
        if ((error as Error).name === 'AbortError') return
        setMessages((prev) => {
          const last = prev[prev.length - 1]
          if (last.role !== 'assistant') return prev
          return [...prev.slice(0, -1), { ...last, content: 'Ошибка соединения', status: 'done' }]
        })
      } finally {
        setIsStreaming(false)
      }
    },
    [conversationId, accessToken, isStreaming]
  )

  const stop = useCallback(() => {
    abortRef.current?.abort()
  }, [])

  return { messages, isStreaming, sendMessage, stop }
}
