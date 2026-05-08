---
plan: 19-10
phase: 19
slug: frontend-usechat-split
wave: 5
depends_on: []
files_modified:
  - services/frontend/hooks/useChat.ts
  - services/frontend/app/(app)/chat/[id]/page.tsx
files_created:
  - services/frontend/hooks/usePendingApprovalFlow.ts
  - services/frontend/lib/sse.ts
  - services/frontend/hooks/__tests__/usePendingApprovalFlow.test.ts
  - services/frontend/lib/__tests__/sse.test.ts
files_deleted: []
success_criteria: [SC-01, SC-03]
autonomous: true
estimated_loc_delta: -250 / +320
---

## Plan Goal

Split `services/frontend/hooks/useChat.ts` (444 LOC) into:

- `useChat` — owns `Message[]`, streaming state, `sendMessage`, `stop`, history load. Accepts `onApprovalRequired` callback prop. Exposes `appendSSEEvent` for sibling resume-stream wiring.
- `usePendingApprovalFlow` — owns `pendingApproval` state, `setPending`, `resolveApproval`, hydration from `GET /messages.pendingApprovals`. Accepts `onResumeEvent` callback wired to `chat.appendSSEEvent` by the parent.
- Pure helpers (`parseSSELine`, `applySSEEvent`, `consumeSSEStream`) move to `services/frontend/lib/sse.ts` (RESEARCH §16 Q4).

Wired together in `app/(app)/chat/[id]/page.tsx`: parent connects `onApprovalRequired={approvalFlow.setPending}` and `onResumeEvent={chat.appendSSEEvent}`. Single source of truth for messages remains `useChat`; approval state remains in `usePendingApprovalFlow`. Phase 17 contracts preserved (D-19).

Implements: D-19, R4 (callback wiring), Q4 (`lib/sse.ts` location), SC-01, SC-03.

<context>
@.planning/phases/19-modular-decomposition/SPEC.md
@.planning/phases/19-modular-decomposition/19-CONTEXT.md
@.planning/phases/19-modular-decomposition/19-RESEARCH.md
@.planning/phases/19-modular-decomposition/19-PATTERNS.md
@.planning/phases/17-hitl-frontend/17-CONTEXT.md
@services/frontend/hooks/useChat.ts
@services/frontend/AGENTS.md
@docs/frontend-style.md
@docs/frontend-patterns.md
</context>

<tasks>

<task type="auto">
  <id>19-10-01</id>
  <title>Extract pure SSE helpers into services/frontend/lib/sse.ts</title>
  <wave>1</wave>
  <read_first>
    - services/frontend/hooks/useChat.ts:16-103 (parseSSELine, applySSEEvent, consumeSSEStream — current bodies)
    - services/frontend/hooks/__tests__/useChat.test.ts (existing helper tests; D-16)
    - services/frontend/lib/api.ts (existing lib pattern)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("services/frontend/lib/sse.ts" lines 994-1029)
  </read_first>
  <action>
    1. Create `services/frontend/lib/sse.ts` containing the three pure helpers (`parseSSELine`, `applySSEEvent`, `consumeSSEStream`) lifted byte-for-byte from `useChat.ts:16-103`. Match existing project import idiom by checking `services/frontend/lib/api.ts` (path alias `@/lib/...`).

    2. Delete the three helper definitions from `useChat.ts`. Replace with `import { parseSSELine, applySSEEvent, consumeSSEStream } from '@/lib/sse'`.

    3. If existing tests in `services/frontend/hooks/__tests__/useChat.test.ts` test these helpers directly, move them to `services/frontend/lib/__tests__/sse.test.ts` (file move + import-path update only; assertions byte-identical per D-16).

    4. Add new file `services/frontend/lib/__tests__/sse.test.ts` covering at least 6 cases:
       - `parseSSELine returns null for non-data lines`
       - `parseSSELine returns null for malformed JSON`
       - `parseSSELine parses well-formed SSE data line`
       - `applySSEEvent appends text content for type=text events`
       - `applySSEEvent records tool_call into msg.toolCalls[]`
       - `applySSEEvent updates tool_result by call_id`

       Use vitest + @testing-library style consistent with rest of frontend tests.

    Apply project conventions:
    - `function` declarations (not arrow consts) per frontend AGENTS.md
    - Imports grouped: React → third-party → internal (`@/lib`, `@/hooks`)
    - Pure functions only — no hooks

    Anti-pattern: do NOT keep duplicate helper copies in `useChat.ts`. Single source of truth.

    Commit subject: `refactor(19): extract pure SSE helpers into lib/sse.ts`.
  </action>
  <acceptance_criteria>
    - File `services/frontend/lib/sse.ts` exists with `parseSSELine`, `applySSEEvent`, `consumeSSEStream` exports
    - File `services/frontend/lib/__tests__/sse.test.ts` exists with ≥6 test cases
    - `useChat.ts` no longer defines these helpers: `rg -c 'export function parseSSELine|export function applySSEEvent|export async function consumeSSEStream' services/frontend/hooks/useChat.ts` returns 0
    - `useChat.ts` imports from `@/lib/sse`: `rg "from ['\"]@/lib/sse" services/frontend/hooks/useChat.ts` returns ≥1
    - `cd services/frontend && pnpm test --run` exits 0
    - `cd services/frontend && pnpm lint && pnpm typecheck` exits 0
  </acceptance_criteria>
  <automated>cd services/frontend &amp;&amp; pnpm test --run</automated>
</task>

<task type="auto">
  <id>19-10-02</id>
  <title>Split useChat: extract usePendingApprovalFlow + add appendSSEEvent / onApprovalRequired wiring</title>
  <wave>2</wave>
  <read_first>
    - services/frontend/hooks/useChat.ts (current — pendingApproval state, resolveApproval, hydration, normalizePendingApproval, handleSSEEvent)
    - services/frontend/app/(app)/chat/[id]/page.tsx (current ChatPage parent component)
    - .planning/phases/17-hitl-frontend/17-CONTEXT.md (D-10 applySSEEvent reuse, D-11 hydration contract)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("Plan 19-10" lines 1031-1085)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 9 "Frontend: useChat / usePendingApprovalFlow split" lines 1124-1262)
  </read_first>
  <action>
    1. **Create `services/frontend/hooks/usePendingApprovalFlow.ts`** owning the pending-approval slice:

       ```ts
       import { useCallback, useEffect, useRef, useState } from 'react';
       import { applySSEEvent, consumeSSEStream } from '@/lib/sse';
       // ... other imports (useAuthStore, fetcher etc.) matching useChat's current style

       interface UsePendingApprovalFlowOptions {
         conversationId: string;
         onResumeEvent: (event: Record<string, unknown>) => void;
         // optional initial hydration source (e.g., from messages payload)
       }

       export function usePendingApprovalFlow({ conversationId, onResumeEvent }: UsePendingApprovalFlowOptions) {
         const [pendingApproval, setPending] = useState<PendingApproval | null>(null);
         const [isResolving, setIsResolving] = useState(false);
         const isResolvingRef = useRef(false);
         const abortRef = useRef<AbortController | null>(null);
         const accessToken = useAuthStore((s) => s.accessToken);

         // Hydration from GET /messages.pendingApprovals on mount.
         // Lift body verbatim from useChat.ts:226-233.
         useEffect(() => { /* ... */ }, [conversationId, accessToken]);

         const resolveApproval = useCallback(async (decisions: ApprovalDecision[]) => {
           if (!pendingApproval || isResolvingRef.current) return;
           isResolvingRef.current = true;
           setIsResolving(true);

           // Sanitize decisions (verbatim lift from useChat.ts:355-380):
           // - strip 'tool_name' from edited_args
           // - clamp reject_reason to 500 chars
           const sanitized: ApprovalDecision[] = decisions.map((d) => {
             const copy: ApprovalDecision = { id: d.id, action: d.action };
             if (d.action === 'edit' && d.edited_args) {
               const filtered: Record<string, string | number | boolean> = {};
               for (const [k, v] of Object.entries(d.edited_args)) {
                 if (k === 'tool_name') continue;
                 filtered[k] = v;
               }
               copy.edited_args = filtered;
             }
             if (d.action === 'reject' && d.reject_reason !== undefined) {
               copy.reject_reason = d.reject_reason.slice(0, 500);
             }
             return copy;
           });

           try {
             // POST resolve (verbatim lift)
             // Open resume SSE: consumeSSEStream(resp, signal, onResumeEvent)
             // — onResumeEvent is the parent-supplied callback; we no longer
             //   write into a local messages array.
           } finally {
             setPending(null);
             setIsResolving(false);
             isResolvingRef.current = false;
           }
         }, [conversationId, accessToken, pendingApproval, onResumeEvent]);

         return { pendingApproval, setPending, resolveApproval, isResolving };
       }
       ```

       Lift `normalizePendingApproval` (useChat.ts:129-152) into a private helper inside `usePendingApprovalFlow.ts` (or `lib/sse.ts` if it's pure — verify). Sanitization preserved BYTE-IDENTICALLY (RESEARCH §9 — security boundary).

    2. **Slim `services/frontend/hooks/useChat.ts`** to ~250 LOC:
       - Drop `pendingApproval` state, `setPendingApproval`, `resolveApproval`, hydration of pending state, and `normalizePendingApproval` import.
       - Add `onApprovalRequired?: (approval: PendingApproval) => void` to options bag.
       - `handleSSEEvent` for `tool_approval_required` calls `onApprovalRequired?.(approval)` instead of `setPendingApproval`.
       - Expose `appendSSEEvent: (event: Record<string, unknown>) => void` on the return:
         ```ts
         const appendSSEEvent = useCallback((event: Record<string, unknown>) => {
           setMessages((prev) => {
             if (prev.length === 0) return prev;
             const last = prev[prev.length - 1];
             return [...prev.slice(0, -1), applySSEEvent(last, event)];
           });
         }, []);
         ```
       - Return shape: `{ messages, isLoading, isStreaming, sendMessage, stop, appendSSEEvent }`.

    3. **Update `services/frontend/app/(app)/chat/[id]/page.tsx`** to wire the sibling hooks:

       ```tsx
       export default function ChatPage({ params }: { params: { id: string } }) {
         const chat = useChat({
           conversationId: params.id,
           onApprovalRequired: (approval) => approvalFlow.setPending(approval),
         });
         const approvalFlow = usePendingApprovalFlow({
           conversationId: params.id,
           onResumeEvent: chat.appendSSEEvent,
         });

         // Note the forward-reference loop above isn't actually a JS issue —
         // both useChat and usePendingApprovalFlow's callbacks are read at the
         // time of dispatch, not at hook-call time. If the linter complains,
         // capture an opaque ref-callback shape:
         //
         //   const approvalRef = useRef<typeof approvalFlow>();
         //   const chat = useChat({ onApprovalRequired: a => approvalRef.current?.setPending(a) });
         //   const approvalFlow = usePendingApprovalFlow({ onResumeEvent: chat.appendSSEEvent });
         //   useEffect(() => { approvalRef.current = approvalFlow; });
         //
         // Use whichever idiom keeps the ChatPage simple (the project may already
         // have a pattern for sibling hook wiring — check existing pages first).

         return (
           <>
             <MessageList messages={chat.messages} />
             {approvalFlow.pendingApproval && (
               <ToolApprovalCard
                 approval={approvalFlow.pendingApproval}
                 onResolve={approvalFlow.resolveApproval}
               />
             )}
             <Composer
               onSend={chat.sendMessage}
               disabled={chat.isStreaming || approvalFlow.isResolving}
             />
           </>
         );
       }
       ```

    4. **Add tests** at `services/frontend/hooks/__tests__/usePendingApprovalFlow.test.ts` covering at least:
       - `setPending hydrates from GET /messages.pendingApprovals on mount` (mock fetch)
       - `resolveApproval POSTs sanitized decisions and clears pending`
       - `resolveApproval strips tool_name from edited_args before POST` (security boundary — RESEARCH §9)
       - `resolveApproval clamps reject_reason to 500 chars`
       - `resolveApproval calls onResumeEvent for each SSE frame from resume stream`
       - `resolveApproval is no-op while isResolving=true (debounce)`

    5. **Update existing useChat tests** (D-16 import-path-only):
       - Tests of the pendingApproval slice move to `usePendingApprovalFlow.test.ts` with assertions UNCHANGED.
       - Tests of `messages` / `sendMessage` / `stop` stay in `useChat.test.ts`. They may need their setup adapted (no longer providing `setPendingApproval` mock; instead `onApprovalRequired` mock), but assertion bodies unchanged.

    Apply project conventions:
    - `function` declarations
    - React 18 idioms (useCallback, useEffect with deps array)
    - No internal state in dumb components

    Anti-pattern enforcement (RESEARCH §9):
    - Do NOT call `controller.abort()` in `tool_approval_required` SSE handler. Let orchestrator close naturally.
    - Do NOT collapse the two hooks into one combined `useChatWithHITL` — defeats D-19.
    - Do NOT have `useChat` poll persisted approval state — that's the sibling hook's job.

    Commit subject: `refactor(19): split useChat into useChat + usePendingApprovalFlow`.
  </action>
  <acceptance_criteria>
    - File `services/frontend/hooks/usePendingApprovalFlow.ts` exists
    - File `services/frontend/hooks/__tests__/usePendingApprovalFlow.test.ts` exists with ≥6 tests
    - `useChat.ts` no longer owns pending state: `rg 'pendingApproval|setPendingApproval' services/frontend/hooks/useChat.ts` returns 0
    - `useChat.ts` exports `onApprovalRequired` option: `rg 'onApprovalRequired' services/frontend/hooks/useChat.ts` returns ≥1
    - `useChat.ts` exports `appendSSEEvent`: `rg 'appendSSEEvent' services/frontend/hooks/useChat.ts` returns ≥1
    - `useChat.ts` ≤300 LOC: `wc -l services/frontend/hooks/useChat.ts | awk '{exit ($1<=300)?0:1}'`
    - `usePendingApprovalFlow.ts` ≤300 LOC
    - ChatPage wires both hooks: `rg 'usePendingApprovalFlow|appendSSEEvent|setPending' services/frontend/app/\(app\)/chat/\[id\]/page.tsx | wc -l` returns ≥3
    - Sanitization preserved: `rg "tool_name|slice\(0, 500\)" services/frontend/hooks/usePendingApprovalFlow.ts | wc -l` returns ≥2
    - `cd services/frontend && pnpm test --run` exits 0
    - `cd services/frontend && pnpm lint && pnpm typecheck` exits 0
    - `bash scripts/check-loc.sh` no longer flags `useChat.ts`
  </acceptance_criteria>
  <automated>cd services/frontend &amp;&amp; pnpm test --run</automated>
</task>

</tasks>

## Verification

```bash
# SC-01
test "$(wc -l < services/frontend/hooks/useChat.ts)" -le 300
test "$(wc -l < services/frontend/hooks/usePendingApprovalFlow.ts)" -le 300

# SSE helpers single source of truth
test "$(rg -c 'export function parseSSELine|export function applySSEEvent' services/frontend/hooks/useChat.ts)" -eq 0
test "$(rg -c 'export function parseSSELine|export function applySSEEvent' services/frontend/lib/sse.ts)" -ge 2

# SC-03: assertions unchanged where tests stayed
git diff $(git merge-base HEAD main)..HEAD -- services/frontend/hooks/__tests__/useChat.test.ts | rg '^\+\s+expect\(' | wc -l   # context-only

# SC-02
cd services/frontend && pnpm test --run && pnpm lint && pnpm typecheck
```

## Success Criteria

- `useChat.ts` ≤300 LOC; `usePendingApprovalFlow.ts` ≤300 LOC; `lib/sse.ts` ≤200 LOC
- Pure helpers in single location (`lib/sse.ts`); no duplicate copies
- ChatPage wires sibling hooks via `onApprovalRequired` + `onResumeEvent` callbacks
- Sanitization (`tool_name` strip + 500-char clamp) preserved BYTE-IDENTICALLY
- All frontend tests pass with import-path-only updates (SC-03 / D-16)
- `pnpm test --run && pnpm lint && pnpm typecheck` green
