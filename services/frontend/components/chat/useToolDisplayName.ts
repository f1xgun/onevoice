'use client';

import { useTranslations } from 'next-intl';

export function useToolDisplayName(toolName: string, displayNameKey?: string): string {
  const tToolNames = useTranslations('agentTasks.displayName');
  const tCard = useTranslations('chat.toolCard');
  const [platform, action] = toolName.split('__');
  const derivedKey = action ? `tools.${platform}.${action}.name` : undefined;

  for (const key of [displayNameKey, derivedKey]) {
    if (!key || !tToolNames.has(key)) continue;
    const resolved = tToolNames(key);
    if (resolved && resolved !== key && resolved !== `agentTasks.displayName.${key}`) {
      return resolved;
    }
  }
  return tCard('unknownAction');
}
