'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { useMutation } from '@tanstack/react-query'
import { api } from '@/lib/api'

export default function ChatIndexPage() {
  const router = useRouter()

  const { mutate: createConversation } = useMutation({
    mutationFn: () => api.post('/conversations').then((r) => r.data.conversation),
    onSuccess: (conv) => router.replace(`/chat/${conv.id}`),
  })

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { createConversation() }, [])

  return (
    <div className="h-full flex items-center justify-center text-gray-400">
      Создание диалога...
    </div>
  )
}
