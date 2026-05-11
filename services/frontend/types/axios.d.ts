import 'axios';

declare module 'axios' {
  export interface AxiosRequestConfig {
    metadata?: { skipBusinessNotFound?: boolean };
  }
  export interface InternalAxiosRequestConfig {
    metadata?: { skipBusinessNotFound?: boolean };
    _retry?: boolean;
  }
}
