import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';

// Backend contract — services/api/internal/handler/integration.go:
//
//   GET  /api/v1/businesses/{id}/integrations/drift   (authz.PermIntegrationsRead)
//        → 200 IntegrationDrift[]  (handler.driftView JSON, camelCase tags)
//   POST /api/v1/businesses/{id}/integrations/verify  (authz.PermBusinessUpdate)
//        → 202 { status: "repair_started" }
//
// The verify endpoint re-pushes the stored organization profile to every
// connected platform and reschedules a fresh drift check; it is an
// outward-facing write, hence the business-update gate.
//
// Field names are camelCase to match the Go JSON tags on driftView verbatim —
// the UI never re-shapes them.

// IntegrationDrift is one connected channel's sync state. `lastCheckedAt` is
// omitted (undefined) until the reconciler has actually compared the channel:
// a row can exist as merely "pending" with driftDetected=false and no
// lastCheckedAt, which must read as "not checked", never as "in sync".
export interface IntegrationDrift {
  platform: string;
  externalId: string;
  driftDetected: boolean;
  driftFields: string[];
  lastCheckedAt?: string | null;
  nextCheckAt: string;
}

export interface VerifyIntegrationsResponse {
  status: string;
}

export async function fetchIntegrationsDrift(businessId: string): Promise<IntegrationDrift[]> {
  const { data } = await bizApi(businessId).get<unknown>(BIZ_API_PATHS.INTEGRATIONS.DRIFT);
  return Array.isArray(data) ? (data as IntegrationDrift[]) : [];
}

export async function verifyIntegrations(businessId: string): Promise<VerifyIntegrationsResponse> {
  const { data } = await bizApi(businessId).post<VerifyIntegrationsResponse>(
    BIZ_API_PATHS.INTEGRATIONS.VERIFY
  );
  return data;
}
