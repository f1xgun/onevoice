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
  /**
   * Groups the posts fanned out by one cross-platform broadcast turn.
   * Posts sharing a non-empty value were published together; absent for
   * standalone posts and records created before the field existed.
   */
  broadcastGroupId?: string;
  createdAt: string;
}
