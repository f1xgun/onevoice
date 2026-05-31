// Single source of truth for HTTP status codes used by the frontend.
// Keep alphabetised.

export const HTTP_STATUS = {
  BAD_REQUEST: 400,
  UNAUTHORIZED: 401,
  FORBIDDEN: 403,
  NOT_FOUND: 404,
  CONFLICT: 409,
  GONE: 410,
  UNPROCESSABLE_ENTITY: 422,
  LOCKED: 423,
  TOO_MANY_REQUESTS: 429,
} as const;
