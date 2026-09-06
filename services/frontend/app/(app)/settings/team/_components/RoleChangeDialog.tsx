'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import {
  Dialog,
  AppDialog as DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/design-system/AppDialog';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Label } from '@/components/ui/label';
import { useRoleLabel } from '@/lib/hooks/useRoleLabel';
import type { Role } from '@/lib/schemas';

export interface RoleChangeDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  memberName: string;
  currentRoleId: string;
  roles: Role[];
  onSubmit: (newRoleId: string) => Promise<void>;
}

export function RoleChangeDialog({
  open,
  onOpenChange,
  memberName,
  currentRoleId,
  roles,
  onSubmit,
}: RoleChangeDialogProps) {
  const tTeam = useTranslations('team');
  const getRoleLabel = useRoleLabel();
  const [selectedId, setSelectedId] = useState(currentRoleId);
  const [pending, setPending] = useState(false);

  const handleSubmit = async () => {
    if (selectedId === currentRoleId) {
      onOpenChange(false);
      return;
    }
    setPending(true);
    try {
      await onSubmit(selectedId);
      onOpenChange(false);
    } finally {
      setPending(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={pending ? undefined : onOpenChange}>
      <DialogContent className="max-w-md rounded-lg border border-line bg-paper-raised p-6 shadow-ov-2">
        <DialogHeader>
          <DialogTitle className="text-lg font-medium tracking-tight text-ink">
            {tTeam('members.actions.changeRole')}
          </DialogTitle>
          <DialogDescription className="text-sm text-ink-mid">{memberName}</DialogDescription>
        </DialogHeader>
        <div className="mt-6 flex flex-col gap-2">
          <Label htmlFor="role-select" className="text-meta font-medium text-ink">
            {tTeam('invite.fields.role')}
          </Label>
          <Select value={selectedId} onValueChange={setSelectedId}>
            <SelectTrigger
              id="role-select"
              className="h-auto min-h-11 border-control bg-paper-raised text-reading"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent className="border-control bg-card text-ink shadow-overlay">
              {roles.map((r) => (
                <SelectItem key={r.id} value={r.id}>
                  {getRoleLabel(r.name)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <DialogFooter className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={pending}>
            {tTeam('invite.cancel')}
          </Button>
          <Button onClick={() => void handleSubmit()} disabled={pending}>
            {pending ? tTeam('invite.submitting') : tTeam('invite.submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
