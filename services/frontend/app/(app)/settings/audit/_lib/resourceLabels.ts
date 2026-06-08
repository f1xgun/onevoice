// Resource-label helpers. The canonical resource tuple lives in ./types
// (AUDIT_RESOURCES) and mirrors the Resource values in pkg/audit/builders.go.
// The audit log renders the raw `resource` string ("role", "business", …);
// these helpers map the known ones to `audit.resources.<key>` translations
// while letting any unmapped resource fall through to its raw technical
// name instead of throwing a next-intl missing-message error.

import { AUDIT_RESOURCES, type AuditResource } from './types';

const KNOWN_RESOURCES = new Set<string>(AUDIT_RESOURCES);

// isKnownResource narrows an arbitrary backend string to AuditResource so
// callers can decide between a translated label and the raw fallback.
export function isKnownResource(resource: string): resource is AuditResource {
  return KNOWN_RESOURCES.has(resource);
}
