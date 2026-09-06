import { api } from '@/lib/api';
import { API_PATHS } from '@/lib/constants/apiPaths';

// Public marketing-landing capture client. Both endpoints are unauthenticated
// and return 204 with no body, so these resolve to void.

export interface WaitlistPayload {
  email: string;
  sphere?: string;
  pain?: string;
  consent: boolean;
  source?: string;
  plan?: 'pro';
}

export interface ChannelVotePayload {
  channel: string;
  note?: string;
}

export async function joinWaitlist(payload: WaitlistPayload): Promise<void> {
  await api.post(API_PATHS.WAITLIST, payload);
}

export async function voteChannel(payload: ChannelVotePayload): Promise<void> {
  await api.post(API_PATHS.CHANNEL_VOTES, payload);
}
