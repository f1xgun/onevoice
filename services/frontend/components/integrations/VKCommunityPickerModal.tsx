'use client';

import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import {
  Dialog,
  AppDialog as DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/design-system/AppDialog';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import { bizApi } from '@/lib/api/business-api';
import { INTEGRATION_ENDPOINTS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';

interface VKCommunity {
  id: number;
  name: string;
  screen_name: string;
  photo_50: string;
  members_count: number;
}

interface VKCommunityPickerModalProps {
  open: boolean;
  onClose: () => void;
}

// VKCommunityPickerModal is the second step of the VK OAuth connect flow.
// After the user authorizes and the backend redirects to
// ?vk_step=select_community, this modal lists the communities where the user
// is an admin (fetched with the temporary user token stored server-side) and,
// on Continue, GETs the community-scoped auth URL and does a top-level
// redirect that mints the community token. The community callback then
// finalizes the integration and redirects to ?connected=vk, which the page
// already handles.
export function VKCommunityPickerModal({ open, onClose }: VKCommunityPickerModalProps) {
  const tPicker = useTranslations('integrations.vkCommunityPicker');
  const tIntegrations = useTranslations('integrations');
  const [selected, setSelected] = useState<number | null>(null);
  const [continuing, setContinuing] = useState(false);
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);

  const {
    data: communities = [],
    isLoading,
    isError,
  } = useQuery<VKCommunity[]>({
    queryKey: QUERY_KEYS.BUSINESS_VK_COMMUNITIES(activeBusinessId),
    queryFn: () => {
      const path = INTEGRATION_ENDPOINTS.vk?.communities;
      if (!path || !activeBusinessId) return [] as VKCommunity[];
      return bizApi(activeBusinessId)
        .get(path)
        .then((r) => (Array.isArray(r.data) ? r.data : []) as VKCommunity[]);
    },
    enabled: open && !!activeBusinessId,
    retry: false,
  });

  async function handleContinue() {
    if (selected == null || !activeBusinessId) return;
    const authUrlFor = INTEGRATION_ENDPOINTS.vk?.communityAuthUrl;
    if (!authUrlFor) return;
    setContinuing(true);
    try {
      const { data } = await bizApi(activeBusinessId).get<{ url: string }>(
        authUrlFor(String(selected))
      );
      window.location.href = data.url;
    } catch {
      toast.error(tIntegrations('page.vkAuthFailed'));
      setContinuing(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{tPicker('title')}</DialogTitle>
          <DialogDescription>{tPicker('description')}</DialogDescription>
        </DialogHeader>

        {isLoading && (
          <div className="py-8 text-center text-sm text-ink-soft">{tPicker('loading')}</div>
        )}

        {isError && (
          <div className="py-8 text-center text-sm text-red-500">{tPicker('sessionExpired')}</div>
        )}

        {!isLoading && !isError && communities.length === 0 && (
          <div className="py-8 text-center text-sm text-ink-soft">{tPicker('empty')}</div>
        )}

        {!isLoading && !isError && communities.length > 0 && (
          <div className="max-h-64 space-y-2 overflow-y-auto">
            {communities.map((community) => (
              <label
                key={community.id}
                className={`flex cursor-pointer items-center gap-3 rounded-lg border p-3 transition-colors ${
                  selected === community.id
                    ? 'border-blue-500 bg-blue-50'
                    : 'border-gray-200 hover:border-gray-300'
                }`}
              >
                <input
                  type="radio"
                  name="vk-community"
                  value={community.id}
                  checked={selected === community.id}
                  onChange={() => setSelected(community.id)}
                  className="h-4 w-4 text-blue-600"
                />
                <div className="min-w-0 flex-1">
                  <div className="font-medium">{community.name}</div>
                  <div className="truncate text-sm text-gray-500">
                    {tPicker('members', { count: community.members_count })}
                  </div>
                </div>
              </label>
            ))}
          </div>
        )}

        <div className="flex justify-end gap-2 pt-4">
          <Button variant="outline" onClick={onClose}>
            {tPicker('cancel')}
          </Button>
          <Button onClick={handleContinue} disabled={selected == null || continuing || isError}>
            {continuing ? tPicker('continuing') : tPicker('continue')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
