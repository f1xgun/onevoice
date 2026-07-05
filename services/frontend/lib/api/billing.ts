import { bizApi } from '@/lib/api/business-api';
import { BIZ_API_PATHS } from '@/lib/constants/bizApiPaths';

// Backend contract (services/api/internal/handler/billing.go, gated by
// authz.PermBillingRead):
//   GET /api/v1/businesses/{id}/billing/summary → 200 BillingSummary
//
// Read-only usage transparency; there is no payment surface here. Field
// names are snake_case to match the JSON envelope produced by
// service.BillingSummary verbatim — the UI never re-shapes them.

export interface BillingPlan {
  code: string;
  name: string;
  monthly_credits: number;
}

export interface BillingCredits {
  granted: number;
  used: number;
  remaining: number;
  overage: number;
}

export interface BillingUsageThisMonth {
  actions: number;
  spend_usd: number;
  images: number;
}

export interface BillingDailySpend {
  today_usd: number;
  cap_usd: number;
}

export interface BillingSummary {
  plan: BillingPlan;
  credits: BillingCredits;
  usage_this_month: BillingUsageThisMonth;
  daily_spend: BillingDailySpend;
}

export async function getBillingSummary(businessId: string): Promise<BillingSummary> {
  const { data } = await bizApi(businessId).get<BillingSummary>(BIZ_API_PATHS.BILLING.SUMMARY);
  return data;
}
