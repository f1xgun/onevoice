'use client';

import { useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { Copy } from 'lucide-react';
import { toast } from 'sonner';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { useCreateInvitation } from '@/lib/hooks/useInvitations';
import { useMapInviteError, useMapEmailVerificationError } from '@/lib/resolveErrorMap';
import { useRoleLabel } from '@/lib/hooks/useRoleLabel';
import type { Role } from '@/lib/schemas';

const EXPIRY_OPTIONS = ['3600', '86400', '604800', '2592000'] as const;
type ExpiryValue = (typeof EXPIRY_OPTIONS)[number];

const schema = z.object({
  roleId: z.string().min(1),
  expiresIn: z.enum(EXPIRY_OPTIONS),
});
type FormInput = z.infer<typeof schema>;

type ModalState =
  | { kind: 'form' }
  | { kind: 'copy'; url: string; durationLabel: string; invitationId: string; rawToken: string };

export interface InviteModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  businessId: string;
  roles: Role[];
  defaultRoleId?: string;
  onInvitationCreated: (invitationId: string, rawToken: string) => void;
}

export function InviteModal({
  open,
  onOpenChange,
  businessId,
  roles,
  defaultRoleId,
  onInvitationCreated,
}: InviteModalProps) {
  const tInvite = useTranslations('team.invite');
  const tInviteCopy = useTranslations('team.invite.copy');
  const tExpiry = useTranslations('team.invite.expiryOptions');
  const mapInviteError = useMapInviteError();
  const mapVerifyError = useMapEmailVerificationError();
  const getRoleLabel = useRoleLabel();
  const [state, setState] = useState<ModalState>({ kind: 'form' });
  const [inlineError, setInlineError] = useState<string | null>(null);
  const copyButtonRef = useRef<HTMLButtonElement | null>(null);

  const initialRoleId =
    defaultRoleId ?? roles.find((r) => r.name === 'viewer')?.id ?? roles[0]?.id ?? '';

  const {
    handleSubmit,
    setValue,
    watch,
    reset,
    formState: { isSubmitting },
  } = useForm<FormInput>({
    resolver: zodResolver(schema),
    defaultValues: { roleId: initialRoleId, expiresIn: '604800' },
  });

  const create = useCreateInvitation(businessId);
  const currentRoleId = watch('roleId');
  const currentExpiresIn = watch('expiresIn');

  const expiryLabel = (v: ExpiryValue) => {
    const map: Record<ExpiryValue, string> = {
      '3600': tExpiry('1h'),
      '86400': tExpiry('1d'),
      '604800': tExpiry('7d'),
      '2592000': tExpiry('30d'),
    };
    return map[v];
  };

  const onSubmit = async (data: FormInput) => {
    setInlineError(null);
    try {
      const res = await create.mutateAsync({
        roleId: data.roleId,
        expiresIn: Number(data.expiresIn),
      });
      const url = `${window.location.origin}/invite/${res.token}`;
      const durationLabel = expiryLabel(data.expiresIn);
      onInvitationCreated(res.id, res.token);
      setState({ kind: 'copy', url, durationLabel, invitationId: res.id, rawToken: res.token });
    } catch (err) {
      setInlineError(mapVerifyError(err) ?? mapInviteError(err));
    }
  };

  const handleClose = (next: boolean) => {
    if (!next) {
      reset({ roleId: initialRoleId, expiresIn: '604800' });
      setInlineError(null);
      setState({ kind: 'form' });
    }
    onOpenChange(next);
  };

  const url = state.kind === 'copy' ? state.url : null;
  useEffect(() => {
    if (!url) return;
    let cancelled = false;
    void (async () => {
      try {
        await navigator.clipboard.writeText(url);
        if (!cancelled) toast.success(tInviteCopy('toastSuccess'));
      } catch {
        if (!cancelled) toast.info(tInviteCopy('autoCopyFallback'));
      }
    })();
    const raf = requestAnimationFrame(() => {
      if (!cancelled) copyButtonRef.current?.focus();
    });
    return () => {
      cancelled = true;
      cancelAnimationFrame(raf);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url]);

  const handleManualCopy = async () => {
    if (state.kind !== 'copy') return;
    try {
      await navigator.clipboard.writeText(state.url);
      toast.success(tInviteCopy('toastSuccess'));
    } catch {
      toast.error(tInviteCopy('toastError'));
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-md rounded-lg border border-line bg-paper-raised p-6 shadow-ov-2">
        {state.kind === 'form' ? (
          <>
            <DialogHeader>
              <DialogTitle className="text-lg font-medium tracking-tight text-ink">
                {tInvite('modal.title')}
              </DialogTitle>
              <DialogDescription className="mt-1 text-sm text-ink-mid">
                {tInvite('modal.description')}
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={handleSubmit(onSubmit)} className="mt-6 flex flex-col gap-4">
              <div className="flex flex-col gap-1.5">
                <Label className="text-xs font-medium text-ink-mid">{tInvite('fields.role')}</Label>
                <Select value={currentRoleId} onValueChange={(v) => setValue('roleId', v)}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {roles.map((r) => (
                      <SelectItem key={r.id} value={r.id}>
                        {getRoleLabel(r.name)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className="text-xs text-ink-soft">{tInvite('fields.roleHelper')}</p>
              </div>
              <div className="flex flex-col gap-1.5">
                <Label className="text-xs font-medium text-ink-mid">
                  {tInvite('fields.expiry')}
                </Label>
                <Select
                  value={currentExpiresIn}
                  onValueChange={(v) => setValue('expiresIn', v as ExpiryValue)}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {EXPIRY_OPTIONS.map((v) => (
                      <SelectItem key={v} value={v}>
                        {expiryLabel(v)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {inlineError && (
                <p className="text-sm text-danger" role="alert">
                  {inlineError}
                </p>
              )}
              <DialogFooter className="mt-2 flex justify-end gap-2">
                <Button variant="ghost" type="button" onClick={() => handleClose(false)}>
                  {tInvite('cancel')}
                </Button>
                <Button type="submit" disabled={isSubmitting || create.isPending}>
                  {create.isPending ? tInvite('submitting') : tInvite('submit')}
                </Button>
              </DialogFooter>
            </form>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle className="text-lg font-medium tracking-tight text-ink">
                {tInviteCopy('title')}
              </DialogTitle>
              <DialogDescription className="mt-1 text-sm text-ink-mid">
                {tInviteCopy('description', { duration: state.durationLabel })}
              </DialogDescription>
            </DialogHeader>
            <div className="mt-6 flex flex-col gap-3">
              <div className="flex items-center gap-2">
                <Input
                  readOnly
                  value={state.url}
                  aria-readonly="true"
                  aria-label={tInviteCopy('inputAria')}
                  onFocus={(e) => e.currentTarget.select()}
                  className="font-mono text-xs"
                />
                <Button
                  ref={copyButtonRef}
                  variant="secondary"
                  size="sm"
                  onClick={() => void handleManualCopy()}
                  aria-label={tInviteCopy('buttonAria')}
                >
                  <Copy size={14} className="mr-1" />
                  {tInviteCopy('button')}
                </Button>
              </div>
              <p className="text-xs text-ink-soft">{tInviteCopy('warning')}</p>
            </div>
            <DialogFooter className="mt-6 flex justify-end">
              <Button onClick={() => handleClose(false)}>{tInviteCopy('done')}</Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
