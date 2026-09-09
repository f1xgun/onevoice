import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';

export interface DriftAlertSettings {
  enabled: boolean;
  locale: 'ru' | 'en';
}

export async function fetchDriftAlerts(businessId: string): Promise<DriftAlertSettings> {
  const { data } = await bizApi(businessId).get<DriftAlertSettings>(
    BIZ_API_PATHS.BUSINESS.DRIFT_ALERTS
  );
  return data;
}

export async function updateDriftAlerts(
  businessId: string,
  settings: DriftAlertSettings
): Promise<DriftAlertSettings> {
  const { data } = await bizApi(businessId).put<DriftAlertSettings>(
    BIZ_API_PATHS.BUSINESS.DRIFT_ALERTS,
    settings
  );
  return data;
}
