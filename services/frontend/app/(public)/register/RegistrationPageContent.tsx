import Link from 'next/link';
import { useTranslations } from 'next-intl';

import { AuthShell } from '@/components/auth/AuthShell';
import { ActionButton as Button } from '@/components/design-system/ActionButton';
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
    <AuthShell eyebrow={t('eyebrow')} title={t('title')} description={t('description')}>
      <div className="flex flex-col gap-5">
        <p className="text-reading text-ink">{t('body')}</p>
        <Button asChild size="lg" variant="primary" className="w-full">
          <Link href="/#waitlist">{t('waitlistCta')}</Link>
        </Button>
        <p className="text-sm text-ink-soft">
          {t('haveInvite')}{' '}
          <Link
            href="/register?invited=1"
            className="font-medium text-ink underline hover:no-underline"
          >
            {t('inviteCta')}
          </Link>
        </p>
        <p className="text-sm text-ink-soft">
          {t('haveAccount')}{' '}
          <Link href="/login" className="font-medium text-ink underline hover:no-underline">
            {t('loginCta')}
          </Link>
        </p>
      </div>
    </AuthShell>
  );
}
