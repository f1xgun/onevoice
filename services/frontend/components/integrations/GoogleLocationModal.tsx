'use client';

import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { toast } from 'sonner';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { bizApi } from '@/lib/api/business-api';
import { INTEGRATION_ENDPOINTS } from '@/lib/constants/bizApiPaths';
import { QUERY_KEYS } from '@/lib/constants/queryKeys';
import { useBusinessStore } from '@/lib/stores/business';
import { useMapEmailVerificationError } from '@/lib/resolveErrorMap';

interface GoogleLocation {
  name: string;
  title: string;
  accountId: string;
  storefrontAddress?: {
    addressLines?: string[];
    locality?: string;
    regionCode?: string;
  };
}

interface GoogleLocationModalProps {
  open: boolean;
  onClose: () => void;
}

export function GoogleLocationModal({ open, onClose }: GoogleLocationModalProps) {
  const tGoogle = useTranslations('integrations.googleLocation');
  const [selectedLocation, setSelectedLocation] = useState<string | null>(null);
  const [selectedAccountId, setSelectedAccountId] = useState<string | null>(null);
  const qc = useQueryClient();
  const activeBusinessId = useBusinessStore((s) => s.activeBusinessId);
  const mapVerifyError = useMapEmailVerificationError();

  const {
    data: locations = [],
    isLoading,
    isError,
  } = useQuery<GoogleLocation[]>({
    queryKey: ['businesses', activeBusinessId, 'google-locations'],
    queryFn: () =>
      bizApi(activeBusinessId!)
        .get(INTEGRATION_ENDPOINTS.google_business?.locations ?? '')
        .then((r) => r.data as GoogleLocation[]),
    enabled: open && !!activeBusinessId,
  });

  const tIntegrations = useTranslations('integrations');
  const connectMutation = useMutation({
    mutationFn: (params: { account_id: string; location_id: string }) => {
      if (!activeBusinessId) return Promise.reject(new Error('No active business'));
      const selectPath = INTEGRATION_ENDPOINTS.google_business?.selectLocation;
      if (!selectPath) return Promise.reject(new Error('google_business not configured'));
      return bizApi(activeBusinessId).post(selectPath, params);
    },
    onSuccess: () => {
      toast.success(tIntegrations('googleConnected'));
      qc.invalidateQueries({ queryKey: QUERY_KEYS.BUSINESS_INTEGRATIONS(activeBusinessId) });
      onClose();
    },
    onError: (err: unknown) => {
      toast.error(mapVerifyError(err) ?? tGoogle('connectFailed'));
    },
  });

  function handleSelect() {
    if (!selectedLocation || !selectedAccountId) return;
    connectMutation.mutate({
      account_id: selectedAccountId,
      location_id: selectedLocation,
    });
  }

  function formatAddress(loc: GoogleLocation): string {
    if (!loc.storefrontAddress) return '';
    const parts: string[] = [];
    if (loc.storefrontAddress.addressLines) {
      parts.push(...loc.storefrontAddress.addressLines);
    }
    if (loc.storefrontAddress.locality) {
      parts.push(loc.storefrontAddress.locality);
    }
    return parts.join(', ');
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{tGoogle('title')}</DialogTitle>
          <DialogDescription>{tGoogle('description')}</DialogDescription>
        </DialogHeader>

        {isLoading && (
          <div className="py-8 text-center text-sm text-gray-500">Загрузка локаций...</div>
        )}

        {isError && (
          <div className="py-8 text-center text-sm text-red-500">{tGoogle('sessionExpired')}</div>
        )}

        {!isLoading && !isError && locations.length === 0 && (
          <div className="py-8 text-center text-sm text-gray-500">{tGoogle('noLocations')}</div>
        )}

        {!isLoading && !isError && locations.length > 0 && (
          <div className="max-h-64 space-y-2 overflow-y-auto">
            {locations.map((loc) => (
              <label
                key={loc.name}
                className={`flex cursor-pointer items-center gap-3 rounded-lg border p-3 transition-colors ${
                  selectedLocation === loc.name
                    ? 'border-blue-500 bg-blue-50'
                    : 'border-gray-200 hover:border-gray-300'
                }`}
              >
                <input
                  type="radio"
                  name="google-location"
                  value={loc.name}
                  checked={selectedLocation === loc.name}
                  onChange={() => {
                    setSelectedLocation(loc.name);
                    setSelectedAccountId(loc.accountId);
                  }}
                  className="h-4 w-4 text-blue-600"
                />
                <div className="min-w-0 flex-1">
                  <div className="font-medium">{loc.title}</div>
                  {formatAddress(loc) && (
                    <div className="truncate text-sm text-gray-500">{formatAddress(loc)}</div>
                  )}
                </div>
              </label>
            ))}
          </div>
        )}

        <div className="flex justify-end gap-2 pt-4">
          <Button variant="outline" onClick={onClose}>
            {tGoogle('cancel')}
          </Button>
          <Button onClick={handleSelect} disabled={!selectedLocation || connectMutation.isPending}>
            {connectMutation.isPending ? tGoogle('connecting') : tGoogle('connect')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
