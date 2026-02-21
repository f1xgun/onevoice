'use client';

import { ChatWindow } from '@/components/chat/ChatWindow';

export default function ConversationPage({ params }: { params: { id: string } }) {
  return <ChatWindow conversationId={params.id} />;
}
