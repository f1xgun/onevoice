// @vitest-environment node

import { createRequire } from 'node:module';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const requireConfig = createRequire(import.meta.url);
const configPath = requireConfig.resolve('../../next.config.js');
const captchaSources = 'https://smartcaptcha.yandexcloud.net https://smartcaptcha.cloud.yandex.ru';

interface HeaderRule {
  source: string;
  headers: { key: string; value: string }[];
}

function loadConfig(): { poweredByHeader: boolean; headers(): Promise<HeaderRule[]> } {
  delete requireConfig.cache[configPath];
  return requireConfig(configPath);
}

beforeEach(() => {
  vi.stubEnv('NODE_ENV', 'production');
  vi.stubEnv('NEXT_PUBLIC_API_URL', '');
  vi.stubEnv('API_URL', '');
  vi.stubEnv('PUBLIC_URL', 'http://localhost');
});

afterEach(() => {
  vi.unstubAllEnvs();
  delete requireConfig.cache[configPath];
});

describe('generated security headers', () => {
  it.each([
    ['default same-origin', '', '', "'self'"],
    ['relative public API overrides internal proxy', '/api/v1', 'http://api:8080', "'self'"],
    ['absolute same-origin', 'http://localhost/api/v1', '', "'self'"],
    [
      'cross-origin public API',
      'https://api.example.test/api/v1',
      '',
      "'self' https://api.example.test",
    ],
    ['proxy fallback', '', 'https://api.example.test/api/v1', "'self' https://api.example.test"],
    [
      'public API takes precedence',
      'https://api.example.test/api/v1',
      'http://api:8080',
      "'self' https://api.example.test",
    ],
    [
      'custom port and path',
      'https://api.example.test:8443/api/v1?test=1#fragment',
      '',
      "'self' https://api.example.test:8443",
    ],
  ])(
    '%s allows only the configured API origin for login, SSE and uploads',
    async (_, publicApi, api, sources) => {
      vi.stubEnv('NEXT_PUBLIC_API_URL', publicApi);
      vi.stubEnv('API_URL', api);
      const [rule] = await loadConfig().headers();
      const csp = rule.headers.find(({ key }) => key === 'Content-Security-Policy')!.value;
      expect(csp.split('; ').find((directive) => directive.startsWith('connect-src '))).toBe(
        `connect-src ${sources} ${captchaSources}`
      );
    }
  );

  it('keeps an absolute API on the public deployment origin covered by self', async () => {
    vi.stubEnv('PUBLIC_URL', 'https://app.example.test');
    vi.stubEnv('NEXT_PUBLIC_API_URL', 'https://app.example.test/api/v1');
    const [rule] = await loadConfig().headers();
    expect(rule.headers[0].value).toContain(`connect-src 'self' ${captchaSources};`);
  });

  it('captures the API origin when the config is loaded', async () => {
    vi.stubEnv('NEXT_PUBLIC_API_URL', 'https://api.example.test/api/v1');
    const config = loadConfig();
    vi.stubEnv('NEXT_PUBLIC_API_URL', 'https://other.example.test/api/v1');
    const [rule] = await config.headers();
    expect(rule.headers[0].value).toContain("connect-src 'self' https://api.example.test ");
    expect(rule.headers[0].value).not.toContain('other.example.test');
  });

  it.each(['data:text/plain,test', 'javascript:alert(1)', 'ftp://api.example.test'])(
    'rejects non-HTTP API URL %s',
    (api) => {
      vi.stubEnv('NEXT_PUBLIC_API_URL', api);
      expect(loadConfig).toThrow('API URL must use HTTP or HTTPS');
    }
  );

  it.each(['production', 'development'])(
    'emits one exact header map without HSTS on HTTP localhost in %s',
    async (mode) => {
      vi.stubEnv('NODE_ENV', mode);
      const config = loadConfig();
      const rules = await config.headers();
      expect(config.poweredByHeader).toBe(false);
      expect(rules).toHaveLength(1);
      expect(rules[0].source).toBe('/:path*');
      const headers = rules[0].headers;
      const names = headers.map(({ key }) => key.toLowerCase());
      expect(new Set(names).size).toBe(names.length);
      expect(names).not.toContain('strict-transport-security');
      expect(Object.fromEntries(headers.map(({ key, value }) => [key, value]))).toEqual({
        'Content-Security-Policy': [
          "default-src 'self'",
          `script-src 'self' 'unsafe-inline' ${captchaSources}${mode === 'development' ? " 'unsafe-eval'" : ''}`,
          "style-src 'self' 'unsafe-inline'",
          "img-src 'self' data: blob: https:",
          "font-src 'self'",
          `connect-src 'self' ${captchaSources}${mode === 'development' ? ' ws: wss:' : ''}`,
          "media-src 'self' blob:",
          "object-src 'none'",
          "base-uri 'self'",
          "form-action 'self'",
          `frame-src ${captchaSources}`,
          "frame-ancestors 'none'",
        ].join('; '),
        'X-Content-Type-Options': 'nosniff',
        'X-Frame-Options': 'DENY',
        'Referrer-Policy': 'strict-origin-when-cross-origin',
        'Permissions-Policy': 'camera=(), microphone=(), geolocation=()',
        'Cross-Origin-Opener-Policy': 'same-origin-allow-popups',
      });
    }
  );
});
