'use client';

import { useRef } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';

import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { trackEvent } from '@/lib/telemetry';

export type RequestedChannel = 'avito' | 'wildberries' | 'ozon' | '2gis';

interface ChannelDemandSummary {
  channels: { channel: string; count: number }[];
}

interface ChannelRequest {
  businessId: string;
  channel: RequestedChannel;
}

/** Reads saved demand before accepting one request per organization and channel. */
export function useChannelDemand(businessId: string | null, canRead: boolean, canWrite: boolean) {
  const qc = useQueryClient();
  const t = useTranslations('integrations.demand');
  const inFlight = useRef(new Set<string>());
  const activeBusiness = useRef(businessId);
  activeBusiness.current = businessId;
  const query = useQuery({
    queryKey: QUERY_KEYS.BUSINESS_CHANNEL_DEMAND(businessId),
    queryFn: async () => {
      const { data } = await bizApi(businessId!).get<ChannelDemandSummary>(
        BIZ_API_PATHS.CHANNEL_REQUESTS.ROOT
      );
      if (!Array.isArray(data?.channels)) throw new Error('Invalid channel demand summary');
      return data;
    },
    enabled: !!businessId && canRead,
    retry: false,
  });
  const mutation = useMutation({
    mutationFn: async ({ businessId: id, channel }: ChannelRequest) => {
      await bizApi(id).post(BIZ_API_PATHS.CHANNEL_REQUESTS.ROOT, { channel });
    },
    onSuccess: (_, { businessId: id, channel }) => {
      qc.setQueryData<ChannelDemandSummary>(QUERY_KEYS.BUSINESS_CHANNEL_DEMAND(id), (old) => ({
        channels: [
          ...(old?.channels ?? []).filter((item) => item.channel !== channel),
          { channel, count: 1 },
        ],
      }));
      trackEvent('activation', 'waitlist_platform', {
        page: '/integrations',
        metadata: { platform: channel, business_id: id },
        businessId: id,
      });
      if (activeBusiness.current === id) toast.success(t('savedToast'));
    },
    onError: (_, { businessId: id }) => {
      if (activeBusiness.current === id) toast.error(t('saveError'));
    },
    onSettled: (_, __, { businessId: id, channel }) => {
      inFlight.current.delete(`${id}:${channel}`);
    },
  });

  function request(channel: RequestedChannel) {
    if (!businessId || !canRead || !canWrite || !query.isSuccess || query.isError) return;
    const saved = qc.getQueryData<ChannelDemandSummary>(
      QUERY_KEYS.BUSINESS_CHANNEL_DEMAND(businessId)
    );
    if (saved?.channels.some((item) => item.channel === channel && item.count > 0)) return;
    const key = `${businessId}:${channel}`;
    if (inFlight.current.has(key)) return;
    inFlight.current.add(key);
    mutation.mutate({ businessId, channel });
  }

  return {
    requested: new Set(
      (query.data?.channels ?? []).filter((item) => item.count > 0).map((item) => item.channel)
    ),
    isReady: !!businessId && canRead && query.isSuccess && !query.isError,
    isError: query.isError,
    isPending: mutation.isPending,
    refetch: query.refetch,
    request,
  };
}
