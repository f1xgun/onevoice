import { useQuery } from '@tanstack/react-query';
import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';
import { contentTemplatesSchema, type ContentTemplate } from '@/types/content-template';

export function contentTemplatesQueryKey(businessId: string | null) {
  return ['businesses', businessId, 'content-templates'] as const;
}

export function useContentTemplates(businessId: string | null, enabled = true) {
  return useQuery<ContentTemplate[]>({
    queryKey: contentTemplatesQueryKey(businessId),
    queryFn: ({ signal }) =>
      bizApi(businessId!)
        .get<ContentTemplate[]>(BIZ_API_PATHS.CONTENT_TEMPLATES.ROOT, { signal })
        .then((response) => contentTemplatesSchema.parse(response.data)),
    enabled: !!businessId && enabled,
  });
}
