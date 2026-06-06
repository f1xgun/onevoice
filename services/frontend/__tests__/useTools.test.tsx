import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import {
  useTools,
  groupByPlatform,
  findToolsForIntegration,
  toolNamesForPlatform,
  toPlatformKey,
  TOOLS_STALE_TIME_MS,
} from '@/lib/hooks/useTools';
import type { Tool } from '@/lib/schemas';

// useTools now reads activeBusinessId from useBusinessStore and calls
// fetchTools(activeBusinessId) which goes through bizApi.
const bizApiGet = vi.fn();
vi.mock('@/lib/api/business-api', () => ({
  bizApi: (bizId: string) => ({
    get: (path: string, config?: unknown) => bizApiGet(bizId, path, config),
    post: vi.fn(),
    put: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
  }),
}));

vi.mock('@/lib/stores/business', () => ({
  useBusinessStore: (selector: (s: { activeBusinessId: string | null }) => unknown) =>
    selector({ activeBusinessId: 'test-biz-id' }),
}));

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function wrapper(client: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

const VALID_PAYLOAD: Tool[] = [
  {
    name: 'telegram__send_channel_post',
    platform: 'telegram',
    floor: 'manual',
    editableFields: ['text', 'parse_mode'],
    description: 'Send a text post to a Telegram channel',
  },
  {
    name: 'telegram__send_channel_photo',
    platform: 'telegram',
    floor: 'manual',
    editableFields: ['caption'],
    description: 'Send a photo to a Telegram channel',
  },
  {
    name: 'vk__publish_post',
    platform: 'vk',
    floor: 'auto',
    editableFields: [],
    description: 'Publish a post to a VK community',
  },
  {
    name: 'yandex_business__reply_review',
    platform: 'yandex_business',
    floor: 'manual',
    editableFields: ['text'],
    description: 'Reply to a Yandex.Business review',
  },
  {
    name: 'google_business__reply_review',
    platform: 'google_business',
    floor: 'forbidden',
    editableFields: [],
    description: 'Reply to a Google Business review',
  },
];

describe('useTools', () => {
  beforeEach(() => {
    bizApiGet.mockReset();
  });

  it('exposes the 5-minute staleTime constant', () => {
    expect(TOOLS_STALE_TIME_MS).toBe(5 * 60 * 1000);
  });

  it('fetches and validates the tools payload, exposing data through the hook', async () => {
    bizApiGet.mockResolvedValueOnce({ data: VALID_PAYLOAD });
    const client = makeClient();

    const { result } = renderHook(() => useTools(), { wrapper: wrapper(client) });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(bizApiGet).toHaveBeenCalledWith('test-biz-id', '/tools', undefined);
    expect(result.current.data).toHaveLength(VALID_PAYLOAD.length);
    expect(result.current.data?.[0]?.name).toBe('telegram__send_channel_post');
  });

  it('surfaces a Zod parse error when the payload is malformed', async () => {
    bizApiGet.mockResolvedValueOnce({ data: [{ name: 'bad_tool', platform: 'telegram' }] });
    const client = makeClient();

    const { result } = renderHook(() => useTools(), { wrapper: wrapper(client) });

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeDefined();
  });
});

describe('toPlatformKey', () => {
  it.each([
    ['telegram', 'telegram'],
    ['vk', 'vk'],
    ['yandex_business', 'yandex_business'],
    ['google_business', 'google_business'],
    ['some_other_platform', 'other'],
  ])('maps %s → %s', (input, expected) => {
    expect(toPlatformKey(input)).toBe(expected);
  });
});

describe('groupByPlatform', () => {
  it('groups tools into telegram/vk/yandex_business/google_business buckets', () => {
    const buckets = groupByPlatform(VALID_PAYLOAD);

    expect(Object.keys(buckets).sort()).toEqual(
      ['google_business', 'other', 'telegram', 'vk', 'yandex_business'].sort()
    );
    expect(buckets.telegram.map((t) => t.name)).toEqual([
      'telegram__send_channel_post',
      'telegram__send_channel_photo',
    ]);
    expect(buckets.vk.map((t) => t.name)).toEqual(['vk__publish_post']);
    expect(buckets.yandex_business.map((t) => t.name)).toEqual(['yandex_business__reply_review']);
    expect(buckets.google_business.map((t) => t.name)).toEqual(['google_business__reply_review']);
    expect(buckets.other).toEqual([]);
  });

  it('places unknown platforms into the `other` bucket', () => {
    const buckets = groupByPlatform([
      {
        name: 'weird__do_thing',
        platform: 'mystery_platform',
        floor: 'manual',
        editableFields: [],
        description: '',
      },
    ]);
    expect(buckets.other.map((t) => t.name)).toEqual(['weird__do_thing']);
    expect(buckets.telegram).toEqual([]);
  });
});

describe('findToolsForIntegration / toolNamesForPlatform', () => {
  it('filters tools whose platform matches the integration string', () => {
    const matches = findToolsForIntegration(VALID_PAYLOAD, 'telegram');
    expect(matches.map((t) => t.name)).toEqual([
      'telegram__send_channel_post',
      'telegram__send_channel_photo',
    ]);
  });

  it('returns only tool NAMES via toolNamesForPlatform (banner-compat shape)', () => {
    expect(toolNamesForPlatform(VALID_PAYLOAD, 'yandex_business')).toEqual([
      'yandex_business__reply_review',
    ]);
    expect(toolNamesForPlatform(VALID_PAYLOAD, 'nonexistent')).toEqual([]);
  });
});
