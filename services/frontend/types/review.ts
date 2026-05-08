// ReviewDraftStatus tracks the AI-draft lifecycle for a review. Empty string
// is the legacy/never-attempted state (treated like "not ready" by the UI).
export type ReviewDraftStatus = '' | 'generating' | 'ready' | 'failed';

export interface Review {
  id: string;
  businessId: string;
  platform: string;
  externalId: string;
  authorName: string;
  rating: number;
  text: string;
  replyText?: string;
  replyStatus: string;
  platformMeta?: Record<string, unknown>;
  createdAt: string;

  // AI-draft fields populated by the api review_drafter service. Present on
  // pending reviews only (UpdateReply clears them when the operator sends).
  draftReply?: string;
  draftStatus?: ReviewDraftStatus;
  draftGeneratedAt?: string;
  draftError?: string;
}
