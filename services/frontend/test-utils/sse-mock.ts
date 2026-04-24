// Shared helper for mocking SSE streams in Vitest tests.
// Pattern from 17-RESEARCH.md §Example 4.
//
// Returns a `Response` whose body is a `ReadableStream` that enqueues the
// provided chunks in order and closes. Tests can pass this to a `fetch` mock
// to exercise `useChat`'s SSE-consuming code paths without a real network.
export function mockSSEResponse(chunks: string[]): Response {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const c of chunks) controller.enqueue(encoder.encode(c));
      controller.close();
    },
  });
  return new Response(stream, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' },
  });
}

// Convenience: build a single SSE `data:` line. `useChat` scans on the `\n`
// boundary (single newline), not the traditional `\n\n`, so this helper
// matches what chat_proxy emits.
export function sseLine(event: Record<string, unknown>): string {
  return 'data: ' + JSON.stringify(event) + '\n';
}
