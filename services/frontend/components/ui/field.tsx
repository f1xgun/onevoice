import { Label } from '@/components/ui/label';

export function Field({
  label,
  htmlFor,
  required,
  error,
  hint,
  className,
  children,
}: {
  label: string;
  htmlFor: string;
  required?: boolean;
  error?: string;
  hint?: string;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <div className={`flex flex-col gap-1.5 ${className ?? ''}`}>
      <Label htmlFor={htmlFor} className="text-xs font-medium text-ink-mid">
        {label}
        {required && <span className="ml-1 text-ochre">*</span>}
      </Label>
      {children}
      {error && <p className="text-xs text-[var(--ov-danger)]">{error}</p>}
      {hint && !error && <p className="text-xs leading-relaxed text-ink-soft">{hint}</p>}
    </div>
  );
}
