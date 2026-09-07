import { cloneElement, isValidElement } from 'react';
import type { ComponentProps, ReactElement } from 'react';
import { Field } from '@/components/ui/field';

export interface AppFieldProps extends ComponentProps<typeof Field> {
  className?: string;
}

export function AppField({ children, error, hint, htmlFor, ...props }: AppFieldProps) {
  const descriptionId = `${htmlFor}-description`;
  return (
    <Field
      {...props}
      htmlFor={htmlFor}
      className={`min-w-0 [&>label]:text-meta ${props.className ?? ''}`}
    >
      {isValidElement(children)
        ? cloneElement(children as ReactElement, {
            'aria-invalid': !!error,
            'aria-describedby': error || hint ? descriptionId : undefined,
          })
        : children}
      {(error || hint) && (
        <p
          id={descriptionId}
          role={error ? 'alert' : undefined}
          className={error ? 'text-meta text-danger' : 'text-meta text-ink-soft'}
        >
          {error || hint}
        </p>
      )}
    </Field>
  );
}
