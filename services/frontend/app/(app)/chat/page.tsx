'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { useMutation } from '@tanstack/react-query';
import { api } from '@/lib/api';

export default function ChatIndexPage() {
  const router = useRouter();

  const { mutate: createConversation } = useMutation({
    mutationFn: () => api.post('/conversations', { title: 'Новый диалог' }).then((r) => r.data),
    onSuccess: (conv) => router.replace(`/chat/${conv.id}`),
  });

  useEffect(() => {
    createConversation();
  }, [createConversation]);

  return (
    <div className="flex h-full items-center justify-center text-gray-400">Создание диалога...</div>
  );
}
