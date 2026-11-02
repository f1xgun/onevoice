'use client';

import { useQuery } from '@tanstack/react-query';
import { z } from 'zod';
import { bizApi } from '@/lib/api/business-api';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';

export const businessVoiceSchema = z.object({
  voiceProfile: z.string(),
  replySignature: z.string().default(''),
});
export type BusinessVoice = z.infer<typeof businessVoiceSchema>;

export function useBusinessVoice(businessId: string | null, allowed = true) {
  return useQuery({
    queryKey: QUERY_KEYS.BUSINESS_VOICE_PROFILE(businessId),
    queryFn: async () =>
      businessVoiceSchema.parse((await bizApi(businessId!).get('/voice-profile')).data),
    enabled: !!businessId && allowed,
    retry: false,
  });
}
