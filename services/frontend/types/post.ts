export interface PlatformResult {
  postId: string;
  url: string;
  status: string;
  error?: string;
}

export interface Post {
  id: string;
  businessId: string;
  content: string;
  mediaUrls?: string[];
  platformResults?: Record<string, PlatformResult>;
  status: string;
  scheduledAt?: string;
  publishedAt?: string;
  createdAt: string;
}
