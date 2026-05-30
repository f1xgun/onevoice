// Surface F: /settings/privacy.
//
// Renders the WithdrawalPanel inside the settings shell. Mirrors
// settings/account/page.tsx for layout consistency.

'use client';

import { WithdrawalPanel } from '@/components/legal/WithdrawalPanel';

export default function PrivacySettingsPage() {
  return (
    <div className="mx-auto max-w-[720px] space-y-12 p-4 md:p-12">
      <WithdrawalPanel />
    </div>
  );
}
