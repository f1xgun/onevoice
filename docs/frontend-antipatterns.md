# Frontend Anti-Patterns — OneVoice Dashboard

Common mistakes to avoid in `services/frontend/`. Most are caught by ESLint / Prettier. For rules, see [frontend-style.md](frontend-style.md). For the correct shape, see [frontend-patterns.md](frontend-patterns.md).

---

## Inline styles instead of Tailwind

```tsx
// BAD: drifts from the design system, not themeable
<div style={{ padding: '16px', marginTop: '8px' }}>

// GOOD
<div className="p-4 mt-2">
```

Same for CSS modules and `styled-components` — the project uses Tailwind exclusively.

## Manual form state instead of react-hook-form

```tsx
// BAD: duplicates validation across forms, no zod inference
const [name, setName] = useState('');
const [error, setError] = useState('');

const handleSubmit = () => {
  if (!name) setError('Required');
  fetch('/api/business', { body: name });  // no error handling, no content-type
};

// GOOD
const { register, handleSubmit, formState: { errors } } = useForm({
  resolver: zodResolver(schema),
});
```

## Global state kept in `useState`

```tsx
// BAD: auth state recreated on every route, lost on remount
const [user, setUser] = useState(null);
const [token, setToken] = useState(null);

// GOOD: Zustand store, one source of truth
const { user, token } = useAuthStore();
```

Rule of thumb: if two unrelated components need the same value, it doesn't belong in `useState`.

## Duplicating server state in Zustand

```tsx
// BAD: conversations fetched once, then stored in Zustand.
// Now you have two sources of truth (fresh API + stale Zustand)
const setConversations = useConvoStore((s) => s.setConversations);
useEffect(() => {
  fetch('/api/v1/conversations').then((r) => r.json()).then(setConversations);
}, []);

// GOOD: React Query — cache, loading, error, revalidation for free
const { data: conversations, isLoading } = useQuery({
  queryKey: ['conversations'],
  queryFn: () => fetch('/api/v1/conversations').then((r) => r.json()),
});
```

Zustand is for **client** state (auth tokens, UI preferences, modal flags). React Query is for **server** state (anything from `/api/v1/*`).

## `useEffect` for data fetching

```tsx
// BAD: manual loading/error/retry; no caching; refetches on every mount
const [data, setData] = useState(null);
const [loading, setLoading] = useState(true);
useEffect(() => {
  fetch('/api/v1/business').then((r) => r.json()).then((d) => {
    setData(d);
    setLoading(false);
  });
}, []);

// GOOD: useQuery
const { data, isLoading } = useQuery({
  queryKey: ['business'],
  queryFn: () => fetch('/api/v1/business').then((r) => r.json()),
});
```

## Missing type imports

```tsx
// BAD: caught by @typescript-eslint/consistent-type-imports
import { User } from '@/types';

// GOOD
import type { User } from '@/types';
```

ESLint will fail the build — don't silence the rule, fix the import.

## `const` arrow for components

```tsx
// BAD: anonymous in stack traces — painful for debugging and React DevTools
const ChatMessage = ({ message }: Props) => { /* ... */ };

// GOOD: named function — shows up as "ChatMessage" in dev tools
function ChatMessage({ message }: Props) { /* ... */ }
```

## Editing `components/ui/` primitives by hand

```tsx
// BAD: changes are lost on next shadcn regenerate
// src/components/ui/button.tsx
export function Button({ fancyColor, ...props }) { /* hand-added prop */ }

// GOOD: wrap the primitive in a feature component
// src/components/fancy-button.tsx
export function FancyButton(props) {
  return <Button className={cn('bg-gradient-to-r from-pink-500', props.className)} {...props} />;
}
```

## Hardcoded backend URLs

```tsx
// BAD: breaks in docker-compose, in prod, and on different machines
const res = await fetch('http://localhost:8080/api/v1/business');

// GOOD: relative path, proxied by next.config.js rewrites
const res = await fetch('/api/v1/business');
```

## Using `any` or leaving props untyped

```tsx
// BAD
export function ChatMessage(props) { /* ... */ }
export function ChatMessage(props: any) { /* ... */ }

// GOOD
interface ChatMessageProps {
  message: Message;
  onRetry?: (id: string) => void;
}
export function ChatMessage({ message, onRetry }: ChatMessageProps) { /* ... */ }
```

## Client-side components doing server work

```tsx
// BAD: 'use client' for a component that just renders static content from an API call
'use client';
export function BusinessHeader() {
  const { data } = useQuery({ queryKey: ['business'], queryFn: getBusiness });
  return <h1>{data?.name}</h1>;
}

// GOOD: Server Component — fetch on the server, no client-side waterfall
export default async function BusinessHeader() {
  const business = await getBusiness();
  return <h1>{business.name}</h1>;
}
```

Add `"use client"` only when you need hooks, events, browser APIs, or state.

## Error alerts instead of inline feedback

```tsx
// BAD: jarring, blocks the UI, not accessible
if (!res.ok) alert('Failed to save');

// GOOD: inline message in the form, or toast notification for global errors
setError('root', { message: 'Failed to save — please retry' });
// or
toast.error('Failed to save');
```

## Forgetting loading and error states

```tsx
// BAD
const { data } = useQuery({ queryKey: ['business'], queryFn: getBusiness });
return <h1>{data.name}</h1>;  // crashes on first render (data is undefined)

// GOOD
const { data, isLoading, error } = useQuery({ queryKey: ['business'], queryFn: getBusiness });
if (isLoading) return <Skeleton />;
if (error) return <ErrorBanner error={error} />;
return <h1>{data.name}</h1>;
```
