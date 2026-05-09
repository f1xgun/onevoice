import type { AxiosRequestConfig } from 'axios';

import { api } from '@/lib/api';

/**
 * bizApi prefixes every business-scoped path with /businesses/${bizId}/.
 * Throws if bizId is empty so a missing activeBusinessId surfaces as a
 * loud programmer error rather than a request to /businesses/undefined/...
 *
 * Example:
 *   const { data } = await bizApi(activeBusinessId).get<Integration[]>('/integrations');
 */
export function bizApi(bizId: string) {
  if (!bizId) {
    throw new Error('bizApi: activeBusinessId is required');
  }
  const prefix = `/businesses/${bizId}`;
  return {
    get: <T = unknown>(path: string, config?: AxiosRequestConfig) =>
      api.get<T>(`${prefix}${path}`, config),
    post: <T = unknown>(path: string, data?: unknown, config?: AxiosRequestConfig) =>
      api.post<T>(`${prefix}${path}`, data, config),
    put: <T = unknown>(path: string, data?: unknown, config?: AxiosRequestConfig) =>
      api.put<T>(`${prefix}${path}`, data, config),
    patch: <T = unknown>(path: string, data?: unknown, config?: AxiosRequestConfig) =>
      api.patch<T>(`${prefix}${path}`, data, config),
    delete: <T = unknown>(path: string, config?: AxiosRequestConfig) =>
      api.delete<T>(`${prefix}${path}`, config),
  };
}
