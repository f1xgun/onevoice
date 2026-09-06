import { useTranslations } from 'next-intl';
import { FilePenLine } from 'lucide-react';
import { DraftSurface } from '@/components/design-system/DraftSurface';
import { DecisionMark } from '@/components/design-system/DecisionMark';
import { StatusLine } from '@/components/design-system/StatusLine';

export function WorkExample() {
  const t = useTranslations('landing.workExample');
  return (
    <section
      id="work-example"
      aria-labelledby="work-example-title"
      className="border-b border-line"
    >
      <div className="mx-auto w-full max-w-[1120px] px-5 pb-12 pt-4 sm:px-10 md:pb-20">
        <h2 id="work-example-title" className="text-section md:text-section-lg">
          {t('title')}
        </h2>
        <p className="mt-2 text-meta text-ink-soft">{t('demo')}</p>
        <dl className="mt-4 grid min-w-0 gap-3 md:grid-cols-[96px_minmax(0,1fr)] md:gap-6">
          <dt className="text-meta text-ink-soft">{t('requestLabel')}</dt>
          <dd className="max-w-[66ch] text-reading">{t('request')}</dd>
          <dt className="text-meta text-ink-soft">{t('draftLabel')}</dt>
          <dd className="min-w-0">
            <DraftSurface>
              <p className="text-meta text-ink-soft">{t('context')}</p>
              <h3 className="mt-3 text-document-title">{t('draftTitle')}</h3>
              <p className="mt-3 max-w-[66ch]">{t('draft')}</p>
              <p className="mt-4 max-w-[66ch] text-meta">
                {t('editLabel')}: <del>{t('before')}</del> →{' '}
                <ins className="bg-brand-soft text-ink">{t('after')}</ins>
              </p>
            </DraftSurface>
          </dd>
          <dt className="flex flex-col gap-3 text-meta">
            <DecisionMark />
            {t('decisionLabel')}
          </dt>
          <dd className="max-w-[66ch] space-y-3 text-reading">
            <StatusLine role="status" tone="neutral" icon={FilePenLine} text={t('status')} />
            <p>{t('decision')}</p>
            <p className="text-meta text-ink-soft">{t('result')}</p>
          </dd>
        </dl>
      </div>
    </section>
  );
}
