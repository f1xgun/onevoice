// Shared React Query cache TTLs. These values keep the
// frontend cache durations consistent across hooks and prevent the
// magic-number lint rule from firing on `5 * 60 * 1000`-style
// expressions that are repeated in many places.
//
// All values are expressed in milliseconds, matching React Query's
// `staleTime` / `gcTime` API.

const MS_PER_SECOND = 1000;
const SECONDS_PER_MINUTE = 60;
const MS_PER_MINUTE = SECONDS_PER_MINUTE * MS_PER_SECOND;
const FIVE_MINUTES = 5;

/** 5 minutes — used for slow-changing reference data (platform
 * registry, tool registry, business id partition key). */
export const STALE_TIME_5_MIN = FIVE_MINUTES * MS_PER_MINUTE;
