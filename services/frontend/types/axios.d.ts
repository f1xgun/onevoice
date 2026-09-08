import 'axios';

declare module 'axios' {
  export interface AxiosRequestConfig {
    metadata?: { skipBusinessNotFound?: boolean; telemetryBusinessId?: string | null };
  }
  export interface InternalAxiosRequestConfig {
    metadata?: { skipBusinessNotFound?: boolean; telemetryBusinessId?: string | null };
    _retry?: boolean;
  }
}
