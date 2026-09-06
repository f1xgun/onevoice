// @vitest-environment node
import { describe, expect, it } from 'vitest';
import { getPathMatch } from 'next/dist/shared/lib/router/utils/path-match';
import { prepareDestination } from 'next/dist/shared/lib/router/utils/prepare-destination';

import nextConfig from '../next.config.js';

describe('next.config media rewrite', () => {
  it.each(['/media', '/media/'])('rejects bucket root %s', async (path) => {
    const rewrites = await nextConfig.rewrites();
    const media = rewrites.find((rule) => rule.source.startsWith('/media/'))!;
    expect(getPathMatch(media.source)(path)).toBe(false);
  });

  it.each(['logo.png', 'logos/8f0e.png', 'generated/tenant/image.png'])(
    'preserves the known object key %s',
    async (key) => {
      const rewrites = await nextConfig.rewrites();
      const media = rewrites.find((rule) => rule.source.startsWith('/media/'))!;
      const params = getPathMatch(media.source)(`/media/${key}`);
      expect(params).toBeTruthy();
      if (!params) throw new Error('object path did not match');
      const result = prepareDestination({
        destination: media.destination,
        params,
        query: {},
        appendParamsToQuery: false,
      });
      expect(result.parsedDestination.pathname).toBe(
        `/${process.env.S3_BUCKET || 'onevoice'}/${key}`
      );
    }
  );
});
