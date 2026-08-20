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

// isTrustedImageSrc reports whether an image src may be auto-loaded inline.
// Same-origin (relative or matching the current host) and allowlisted hosts pass;
// anything else — and any non-http(s) scheme — is untrusted.
export function isTrustedImageSrc(src: string): boolean {
  const s = src.trim();
  if (s === '') return false;
  // Same-origin relative path ("/media/..."), but never a protocol-relative "//host".
  if (s.startsWith('/') && !s.startsWith('//')) return true;

  let url: URL;
  try {
    url = new URL(s, 'https://placeholder.invalid');
  } catch {
    return false;
  }
  if (url.protocol !== 'https:' && url.protocol !== 'http:') return false;

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
// scheme); otherwise it returns undefined and the caller renders plain text.
export function safeExternalHref(src: string): string | undefined {
  const s = src.trim();
  if (s === '') return undefined;
  if (s.startsWith('/') && !s.startsWith('//')) return s;
  try {
    const url = new URL(s, 'https://placeholder.invalid');
    if (url.protocol === 'https:' || url.protocol === 'http:') return s;
  } catch {
    return undefined;
  }
  return undefined;
}
