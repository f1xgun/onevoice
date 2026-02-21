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
}
