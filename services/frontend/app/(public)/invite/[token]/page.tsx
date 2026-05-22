'use client';

import { useEffect, useState } from 'react';
import { useRouter, useParams } from 'next/navigation';
import Link from 'next/link';
import { useLocale, useTranslations } from 'next-intl';
import { format, parseISO } from 'date-fns';
import type { AxiosError } from 'axios';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { useInvitationPreview, useAcceptInvitation } from '@/lib/hooks/useInvitations';
import { useMapInviteError } from '@/lib/resolveErrorMap';
import { RolePill } from '@/components/business-switcher/RolePill';
import { RefusalCard } from '@/components/invite/RefusalCard';
import { useAuthStore } from '@/lib/auth';
import { HTTP_STATUS } from '@/lib/constants/httpStatus';
import { getDateFnsLocale } from '@/lib/dateFnsLocale';
import type { Locale } from '@/lib/i18n/locales';

export default function AcceptInvitePage() {
  const { token } = useParams<{ token: string }>();
  const router = useRouter();
  const tInvite = useTranslations('invite.accept');
  const dateFnsLocale = getDateFnsLocale(useLocale() as Locale);
  const mapInviteError = useMapInviteError();

  const isAuthed = useAuthStore((s) => s.isAuthenticated);

  useEffect(() => {
    if (!isAuthed) {
      router.replace(`/login?next=/invite/${token}`);
    }
  }, [isAuthed, router, token]);

  const preview = useInvitationPreview(token, isAuthed);
  const accept = useAcceptInvitation();
  const [acceptError, setAcceptError] = useState<AxiosError | null>(null);

  const handleAccept = async () => {
    setAcceptError(null);
    try {
      await accept.mutateAsync(token);
      router.push('/chat');
    } catch (err) {
      const axiosErr = err as AxiosError;
      const status = axiosErr.response?.status;
      if (status === HTTP_STATUS.GONE || status === HTTP_STATUS.CONFLICT) {
        setAcceptError(axiosErr);
      } else {
        toast.error(mapInviteError(err));
      }
    }
  };

  if (!isAuthed) {
    return null;
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-paper px-4 py-12">
      <div className="w-full max-w-md rounded-lg border border-line bg-paper-raised p-8 shadow-ov-2">
        {preview.isLoading && (
          <div className="flex flex-col gap-4">
            <Skeleton className="h-3 w-1/2" />
            <Skeleton className="h-6 w-3/4" />
            <Skeleton className="h-9 w-full" />
          </div>
        )}

        {preview.isError &&
          (preview.error as AxiosError | undefined)?.response?.status === HTTP_STATUS.GONE && (
            <RefusalCard status={HTTP_STATUS.GONE} />
          )}

        {acceptError?.response?.status === HTTP_STATUS.GONE && (
          <RefusalCard status={HTTP_STATUS.GONE} />
        )}
        {acceptError?.response?.status === HTTP_STATUS.CONFLICT && preview.data && (
          <RefusalCard
            status={HTTP_STATUS.CONFLICT}
            businessId={preview.data.business_id}
            businessName={preview.data.business_name}
            onOpenBusiness={() => router.push('/chat')}
          />
        )}

        {!preview.isLoading && !preview.isError && !acceptError && preview.data && (
          <div className="flex flex-col gap-4">
            <p className="text-[11px] font-medium uppercase tracking-[0.04em] text-ink-soft">
              {tInvite('kicker')}
            </p>
            <h1 className="text-2xl font-medium tracking-tight text-ink">
              {preview.data.business_name}
            </h1>
            <RolePill roleName={preview.data.role_name} size="lg" />
            <p className="text-sm text-ink-mid">
              {tInvite('expiry', {
                date: format(parseISO(preview.data.expires_at), 'd MMMM yyyy, HH:mm', {
                  locale: dateFnsLocale,
                }),
              })}
            </p>
            <div className="mt-6 flex flex-col gap-2">
              <Button
                variant="accent"
                size="lg"
                className="w-full"
                onClick={() => void handleAccept()}
                disabled={accept.isPending}
              >
                {accept.isPending ? tInvite('accepting') : tInvite('cta')}
              </Button>
              <Link
                href="/chat"
                className="text-center text-sm text-ink-soft hover:text-ink"
                rel="noreferrer"
              >
                {tInvite('cancel')}
              </Link>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
