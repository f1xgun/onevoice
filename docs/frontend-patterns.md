# Frontend Patterns — OneVoice Dashboard

Good/right-way examples for `services/frontend/` (Next.js 14 App Router, React 18, Tailwind, shadcn/ui, Zustand, TanStack React Query, react-hook-form + zod, Vitest).

For the rules, see [frontend-style.md](frontend-style.md). For mistakes to avoid, see [frontend-antipatterns.md](frontend-antipatterns.md).

---

## Component Shape

```tsx
interface ChatMessageProps {
  message: Message;
  onRetry?: (id: string) => void;
}

export function ChatMessage({ message, onRetry }: ChatMessageProps) {
  const handleRetry = useCallback(() => {
    onRetry?.(message.id);
  }, [message.id, onRetry]);

  return (
    <div className="flex gap-3 p-4">
      <Avatar>{message.role}</Avatar>
      <div className="flex-1">
        <Markdown>{message.content}</Markdown>
        {message.isError && (
          <Button onClick={handleRetry} variant="ghost" size="sm">
            Retry
          </Button>
        )}
      </div>
    </div>
  );
}
```

- `function` declaration (not arrow const) — better stack traces.
- TypeScript interface for props — no `any`.
- Tailwind utility classes only — no inline `style`.
- Hooks memoize event handlers when passed to children.

## Server vs Client Components

Default to Server Components. Add `"use client"` only when you need hooks, events, or browser APIs:

```tsx
// src/app/(dashboard)/business/page.tsx — Server Component
import { getBusiness } from '@/lib/api';
import { BusinessEditor } from '@/components/business-editor';

export default async function BusinessPage() {
  const business = await getBusiness();
  return <BusinessEditor initial={business} />;
}
```

```tsx
// src/components/business-editor.tsx — Client Component
'use client';

import { useForm } from 'react-hook-form';

export function BusinessEditor({ initial }: { initial: Business }) {
  const form = useForm({ defaultValues: initial });
  // ... interactive form
}
```

## Zustand Store

One store per logical domain. Selectors pick the minimum slice to avoid unnecessary rerenders.

```tsx
import { create } from 'zustand';

interface AuthStore {
  user: User | null;
  token: string | null;
  setAuth: (user: User, token: string) => void;
  clearAuth: () => void;
}

export const useAuthStore = create<AuthStore>((set) => ({
  user: null,
  token: null,
  setAuth: (user, token) => set({ user, token }),
  clearAuth: () => set({ user: null, token: null }),
}));

// In a component — subscribe to just the slice you need:
const token = useAuthStore((s) => s.token);
```

## Server State with React Query

Never put server data in Zustand — use React Query. It handles caching, revalidation, and loading/error states for free.

```tsx
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';

export function useConversations() {
  return useQuery({
    queryKey: ['conversations'],
    queryFn: () => fetch('/api/v1/conversations').then((r) => r.json()),
  });
}

export function useCreateConversation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (title: string) =>
      fetch('/api/v1/conversations', {
        method: 'POST',
        body: JSON.stringify({ title }),
      }).then((r) => r.json()),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['conversations'] }),
  });
}
```

## Forms (react-hook-form + zod)

Schema-first, typed via `z.infer`:

```tsx
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';

const businessSchema = z.object({
  name: z.string().min(1, 'Name is required'),
  phone: z.string().regex(/^\+?\d{10,15}$/, 'Invalid phone'),
  email: z.string().email('Invalid email'),
});
type BusinessFormData = z.infer<typeof businessSchema>;

export function BusinessForm() {
  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<BusinessFormData>({ resolver: zodResolver(businessSchema) });

  const onSubmit = async (data: BusinessFormData) => {
    const res = await fetch('/api/v1/business', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error('Failed to save');
  };

  return (
    <form onSubmit={handleSubmit(onSubmit)}>
      <Input {...register('name')} />
      {errors.name && <p className="text-sm text-destructive">{errors.name.message}</p>}
      <Button type="submit" disabled={isSubmitting}>Save</Button>
    </form>
  );
}
```

## Custom Hooks

Extract non-trivial component logic into hooks in `src/hooks/`:

```tsx
// src/hooks/use-chat.ts
export function useChat(conversationId: string) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [isStreaming, setIsStreaming] = useState(false);

  const send = useCallback(async (text: string) => {
    setIsStreaming(true);
    const source = new EventSource(`/chat/${conversationId}?q=${encodeURIComponent(text)}`);
    source.onmessage = (ev) => {
      const event = JSON.parse(ev.data);
      if (event.type === 'done') {
        source.close();
        setIsStreaming(false);
        return;
      }
      setMessages((prev) => applyEvent(prev, event));
    };
  }, [conversationId]);

  return { messages, isStreaming, send };
}
```

## Type Imports

```tsx
// Type-only imports — `import type` is required by ESLint
import type { User, Business } from '@/types';
import { useAuthStore } from '@/stores/auth';
```

## shadcn/ui Primitives

Use generated primitives from `src/components/ui/` directly. Wrap them in feature components, never edit the generated files by hand:

```tsx
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog';

export function ConfirmDelete({ onConfirm }: { onConfirm: () => void }) {
  return (
    <Dialog>
      <DialogContent>
        <DialogTitle>Delete this?</DialogTitle>
        <Button variant="destructive" onClick={onConfirm}>Delete</Button>
      </DialogContent>
    </Dialog>
  );
}
```

Re-run the shadcn CLI to regenerate if a primitive needs an update.

## API Calls via `next.config.js` Rewrites

Never hardcode ports or full URLs in components — `next.config.js` proxies `/api/v1/*` to the API service and `/chat/*` to the orchestrator:

```tsx
// GOOD: relative paths, work in dev and prod
const res = await fetch('/api/v1/business');
```

## Testing with Vitest

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ChatMessage } from './chat-message';

describe('ChatMessage', () => {
  it('renders content and calls onRetry when retry clicked', () => {
    const onRetry = vi.fn();
    render(
      <ChatMessage
        message={{ id: '1', role: 'assistant', content: 'hi', isError: true }}
        onRetry={onRetry}
      />,
    );
    fireEvent.click(screen.getByText('Retry'));
    expect(onRetry).toHaveBeenCalledWith('1');
  });
});
```
