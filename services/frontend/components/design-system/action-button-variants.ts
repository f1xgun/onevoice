import type { ButtonProps } from '@/components/ui/button';
import { buttonVariants } from '@/components/ui/button';
import { cn } from '@/lib/utils';

interface ActionButtonVariantsProps {
  variant?: ButtonProps['variant'];
  size?: ButtonProps['size'];
  className?: string;
}

export function actionButtonVariants({
  variant = 'primary',
  size,
  className,
}: ActionButtonVariantsProps = {}) {
  const primary = !variant || ['primary', 'default', 'accent'].includes(variant);
  const danger = variant === 'danger' || variant === 'destructive';
  return cn(
    buttonVariants({ variant, size }),
    className,
    'min-h-11 min-w-11 h-auto whitespace-normal break-words rounded-md px-4 py-2.5 text-action tracking-normal shadow-none opacity-100 hover:opacity-100 duration-150 active:duration-150 disabled:opacity-100 motion-reduce:transition-none',
    primary
      ? 'bg-brand text-brand-foreground border border-brand hover:bg-brand-hover hover:text-brand-foreground hover:border-brand-hover active:bg-brand-hover'
      : variant === 'link'
        ? 'bg-transparent text-brand underline underline-offset-4 hover:bg-paper-sunken hover:text-brand-hover'
        : cn(
            'bg-paper-raised text-ink border border-control hover:bg-paper-sunken hover:text-ink hover:border-control active:bg-paper-sunken',
            danger && 'text-danger hover:text-danger'
          ),
    size === 'icon' && 'min-h-11 min-w-11 w-auto p-2.5',
    'disabled:bg-paper-sunken disabled:text-ink-soft disabled:border-control disabled:hover:bg-paper-sunken disabled:hover:text-ink-soft disabled:hover:border-control aria-disabled:bg-paper-sunken aria-disabled:text-ink-soft aria-disabled:border-control aria-disabled:opacity-100 aria-disabled:hover:bg-paper-sunken aria-disabled:hover:text-ink-soft aria-disabled:hover:border-control '
  );
}
