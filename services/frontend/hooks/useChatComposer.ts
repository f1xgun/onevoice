import { useEffect, useId, useRef } from 'react';
import type { KeyboardEvent, UIEvent } from 'react';
import { useForm } from 'react-hook-form';
import { z } from 'zod';
import { zodResolver } from '@hookform/resolvers/zod';
import type { Message } from '@/types/chat';

const FOLLOW_THRESHOLD_PX = 80;
const IME_KEY_CODE = 229;
const composerSchema = z.object({ message: z.string() });

interface ChatComposerOptions {
  messages: Message[];
  disabled: boolean;
  sendMessage: (text: string, onAccepted?: () => void) => Promise<void>;
}

export function useChatComposer({ messages, disabled, sendMessage }: ChatComposerOptions) {
  const composerId = useId();
  const { register, watch, setValue } = useForm<z.infer<typeof composerSchema>>({
    resolver: zodResolver(composerSchema),
    defaultValues: { message: '' },
  });
  const input = watch('message');
  const followingRef = useRef(true);
  const sendingRef = useRef(false);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (followingRef.current) bottomRef.current?.scrollIntoView({ behavior: 'auto', block: 'end' });
  }, [messages]);

  async function handleSend() {
    const text = input.trim();
    if (!text || disabled || sendingRef.current) return;
    sendingRef.current = true;
    try {
      await sendMessage(text, () => setValue('message', ''));
    } finally {
      sendingRef.current = false;
    }
  }

  function handleScroll(event: UIEvent<HTMLDivElement>) {
    const el = event.currentTarget;
    followingRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < FOLLOW_THRESHOLD_PX;
  }

  function handleKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (
      event.key === 'Enter' &&
      (event.ctrlKey || event.metaKey) &&
      !event.nativeEvent.isComposing &&
      event.keyCode !== IME_KEY_CODE
    ) {
      event.preventDefault();
      void handleSend();
    }
  }

  return { composerId, register, input, bottomRef, handleSend, handleScroll, handleKeyDown };
}
