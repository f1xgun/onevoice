import 'axios';

declare module 'axios' {
  export interface InternalAxiosRequestConfig {
    metadata?: { skipBusinessNotFound?: boolean };
    _retry?: boolean;
  }
}
