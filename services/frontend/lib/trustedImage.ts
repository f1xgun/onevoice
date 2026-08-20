// lib/trustedImage.ts — gate for images embedded in assistant Markdown.
//
// Assistant messages can echo model output that is influenced by untrusted
// content (review/comment text the model summarizes). A Markdown image there,
// `![x](http://attacker/track.png)`, would be auto-fetched by the browser —
// leaking the viewer's IP, enabling tracking, and handing an attacker-chosen
// host a request. Only first-party / same-origin images are rendered inline;
// everything else is shown as a click-through link the viewer opens on purpose.

// TRUSTED_IMAGE_HOST_SUFFIXES lists first-party media hosts (our object store /
// CDN and the platform CDNs we publish through). A src whose hostname equals or
// ends with `.<suffix>` renders inline. Extend this when a new media host ships.
const TRUSTED_IMAGE_HOST_SUFFIXES: readonly string[] = [];

// RELATIVE_BASE is the sentinel origin used to resolve a relative src. A genuine
// rooted same-origin path resolves back to this exact origin; anything that
// introduces an authority resolves elsewhere and is rejected. Its string value
// equals its own origin (no path / trailing slash), so `url.origin ===
// RELATIVE_BASE` is the same-origin test.
const RELATIVE_BASE = 'https://placeholder.invalid';

// A leading-slash string like `/\host/x` (or `/\\host/x`) is NOT same-origin: the
// WHATWG URL parser treats a backslash as a path separator on special-scheme
// (http/https) pages, so it resolves cross-origin to `https://host/x`. A naive
// `startsWith('/') && !startsWith('//')` prefix check would wrongly trust it and
// the browser would auto-fetch the attacker host. Reject any backslash so such a
// src can never masquerade as a same-origin relative path, then verify the
// resolved origin rather than trusting a string prefix.
function containsBackslash(s: string): boolean {
  return s.includes('\\');
}

// isTrustedImageSrc reports whether an image src may be auto-loaded inline.
// Same-origin (relative or matching the current host) and allowlisted hosts pass;
// anything else — and any non-http(s) scheme — is untrusted.
export function isTrustedImageSrc(src: string): boolean {
  const s = src.trim();
  if (s === '') return false;
  if (containsBackslash(s)) return false;

  let url: URL;
  try {
    url = new URL(s, RELATIVE_BASE);
  } catch {
    return false;
  }
  if (url.protocol !== 'https:' && url.protocol !== 'http:') return false;

  // A rooted same-origin path resolves back to the sentinel base origin.
  if (url.origin === RELATIVE_BASE) return true;

  const host = url.hostname.toLowerCase();
  if (typeof window !== 'undefined' && url.host === window.location.host) {
    return true;
  }
  return TRUSTED_IMAGE_HOST_SUFFIXES.some(
    (suffix) => host === suffix || host.endsWith('.' + suffix)
  );
}

// safeExternalHref returns src only when it is an http(s) URL safe to place in an
// anchor's href (so the placeholder link can never carry a javascript:/data:
// scheme, nor a backslash-obfuscated cross-origin authority); otherwise it
// returns undefined and the caller renders plain text.
export function safeExternalHref(src: string): string | undefined {
  const s = src.trim();
  if (s === '') return undefined;
  if (containsBackslash(s)) return undefined;
  try {
    const url = new URL(s, RELATIVE_BASE);
    if (url.protocol === 'https:' || url.protocol === 'http:') return s;
  } catch {
    return undefined;
  }
  return undefined;
}
