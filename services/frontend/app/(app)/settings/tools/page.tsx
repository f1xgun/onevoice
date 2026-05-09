import { ToolsPageClient } from './ToolsPageClient';

// /settings/tools page.
//
// Lives at app/(app)/settings/tools/page.tsx alongside the existing
// /settings account page. Server component that renders the client
// component which handles React Query + the interactive toggles.
export default function SettingsToolsPage() {
  return <ToolsPageClient />;
}
