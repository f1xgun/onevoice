import { ToolsPageClient } from './ToolsPageClient';

// Phase 16 — /settings/tools (POLICY-05 frontend).
//
// Lives at app/(app)/settings/tools/page.tsx alongside the existing
// /settings account page. Server component that renders the client
// component which handles React Query + the interactive toggles.
export default function SettingsToolsPage() {
  return <ToolsPageClient />;
}
