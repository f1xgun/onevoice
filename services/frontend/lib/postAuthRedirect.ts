// Post-authentication redirect resolution. The login/register pages bounce the
// user to a `?next=` target after a successful auth (e.g. an invite link sends
// unauthenticated visitors to `/login?next=/invite/<token>`), falling back to
// the default landing when absent or untrusted.

const DEFAULT_POST_AUTH_PATH = '/chat';

// Auth pages themselves are never valid `next` targets — redirecting back would
// loop the user straight into the form they just submitted.
const AUTH_PATH_PREFIXES = ['/login', '/register'];

// safeNextPath validates an untrusted redirect target. It only accepts a
// same-origin absolute path: it must start with a single "/" and the second
// character must not be "/" or "\" (both of which browsers resolve as
// protocol-relative external URLs, e.g. "//evil.com"). Anything else — absolute
// URLs, scheme-relative URLs, javascript: payloads, or a bounce back to an auth
// page — falls back to the default landing. This is the open-redirect guard.
export function safeNextPath(raw: string | null | undefined): string {
  if (!raw || raw[0] !== '/') return DEFAULT_POST_AUTH_PATH;
  if (raw[1] === '/' || raw[1] === '\\') return DEFAULT_POST_AUTH_PATH;
  const path = raw.split(/[?#]/, 1)[0];
  if (AUTH_PATH_PREFIXES.includes(path)) return DEFAULT_POST_AUTH_PATH;
  return raw;
}

// nextParamFrom reads the URL-decoded `next` query value from a location search
// string (e.g. window.location.search), or null when absent.
export function nextParamFrom(search: string): string | null {
  return new URLSearchParams(search).get('next');
}

// resolvePostAuthRedirect is the convenience composition used by the auth pages:
// read `next` from the current location and sanitize it in one call.
export function resolvePostAuthRedirect(search: string): string {
  return safeNextPath(nextParamFrom(search));
}

export function loginRedirectPath(location: { pathname: string; search: string }): string {
  return `/login?next=${encodeURIComponent(safeNextPath(location.pathname + location.search))}`;
}
