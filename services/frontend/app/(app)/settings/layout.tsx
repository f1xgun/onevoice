import type { ReactNode } from 'react';

export default function SettingsLayout({ children }: { children: ReactNode }) {
  return <div className="min-w-0">{children}</div>;
}
