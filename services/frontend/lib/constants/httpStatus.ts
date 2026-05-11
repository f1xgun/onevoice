// Single source of truth for HTTP status codes used by the frontend.
// Referenced by axios response interceptors, error-mapping helpers, and
// page-level error handlers that need to differentiate between auth
// failures, conflicts, and forbidden requests. Keep alphabetised.

export const HTTP_STATUS = {
  UNAUTHORIZED: 401,
  FORBIDDEN: 403,
  NOT_FOUND: 404,
  CONFLICT: 409,
  GONE: 410,
  UNPROCESSABLE_ENTITY: 422,
  TOO_MANY_REQUESTS: 429,
} as const;
