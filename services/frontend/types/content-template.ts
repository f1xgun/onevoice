import { z } from 'zod';

export type ContentTemplateKind = 'post' | 'review_reply';

export interface ContentTemplate {
  id: string;
  businessId: string;
  createdBy: string;
  name: string;
  kind: ContentTemplateKind;
  body: string;
  placeholders: string[];
  createdAt: string;
  updatedAt: string;
}

export const contentTemplateSchema = z.object({
  id: z.string().uuid(),
  businessId: z.string().uuid(),
  createdBy: z.string().uuid(),
  name: z.string(),
  kind: z.enum(['post', 'review_reply']),
  body: z.string(),
  placeholders: z.array(z.string()),
  createdAt: z.string(),
  updatedAt: z.string(),
});

export const contentTemplatesSchema = z.array(contentTemplateSchema);
export const renderedContentTemplateSchema = z.object({ content: z.string() });
