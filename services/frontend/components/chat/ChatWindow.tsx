'use client'

import { useRef, useEffect, useState } from 'react'
import { Send } from 'lucide-react'
import { MessageBubble } from './MessageBubble'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useChat } from '@/hooks/useChat'

const QUICK_ACTIONS = ['Проверить отзывы', 'Обновить часы работы', 'Опубликовать пост']

export function ChatWindow({ conversationId }: { conversationId: string }) {
  const { messages, isStreaming, sendMessage } = useChat(conversationId)
  const [input, setInput] = useState('')
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const handleSend = async () => {
    const text = input.trim()
    if (!text || isStreaming) return
    setInput('')
    await sendMessage(text)
  }

  return (
    <div className="flex flex-col h-full">
      {/* Messages */}
      <div className="flex-1 overflow-y-auto p-6">
        {messages.length === 0 ? (
          <div className="h-full flex flex-col items-center justify-center gap-4">
            <p className="text-gray-400 text-lg">Чем могу помочь?</p>
            <div className="flex flex-wrap gap-2 justify-center">
              {QUICK_ACTIONS.map((action) => (
                <button
                  key={action}
                  type="button"
                  onClick={() => sendMessage(action)}
                  className="px-4 py-2 rounded-full border border-gray-200 text-sm text-gray-600 hover:bg-gray-50"
                >
                  {action}
                </button>
              ))}
            </div>
          </div>
        ) : (
          messages.map((msg) => <MessageBubble key={msg.id} message={msg} />)
        )}
        <div ref={bottomRef} />
      </div>

      {/* Input */}
      <div className="border-t bg-white p-4 flex gap-2">
        <Input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && !e.shiftKey && void handleSend()}
          placeholder="Напишите сообщение..."
          disabled={isStreaming}
          className="flex-1"
        />
        <Button onClick={handleSend} disabled={isStreaming || !input.trim()}>
          <Send size={16} />
        </Button>
      </div>
    </div>
  )
}
