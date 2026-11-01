import Link from 'next/link';
import { useTranslations } from 'next-intl';

import { AuthShell } from '@/components/auth/AuthShell';
import { WaitlistForm } from '@/components/landing/WaitlistForm';
import { parseRegistrationMode } from '@/lib/registration-mode';

import { RegisterForm } from './RegisterForm';

interface RegistrationPageContentProps {
  invited?: string | string[];
}

export function RegistrationPageContent({ invited }: RegistrationPageContentProps = {}) {
  const mode = parseRegistrationMode(process.env.REGISTRATION_MODE);
  const hasInvitationLink = invited === '1';

  if (mode === 'invite_only' && !hasInvitationLink) {
    return <ClosedBetaRegistration />;
  }

  return <RegisterForm inviteOnly={mode === 'invite_only'} />;
}

function ClosedBetaRegistration() {
  const t = useTranslations('auth.register.closedBeta');

  return (
    <AuthShell
      variant="access"
      eyebrow={t('eyebrow')}
      title={t('title')}
      description={t('body')}
      footer={
        <>
          <p>
            {t('haveInvite')}
            <br />
            <Link href="/register?invited=1">{t('inviteCta')}</Link>
          </p>
          <p>
            {t('haveAccount')}
            <br />
            <Link href="/login">{t('loginCta')}</Link>
          </p>
        </>
      }
    >
      <WaitlistForm variant="access" submitLabel={t('waitlistCta')} />
    </AuthShell>
  );
}
