import { describe, it, expect } from 'vitest';

import {
  STATUS_LABEL_KEYS,
  STATUS_TONES,
  type IntegrationStatus,
} from '@/lib/constants/integrationStatus';

describe('integration status vocabulary', () => {
  it('maps active to the success tone', () => {
    expect(STATUS_TONES.active).toBe('success');
  });

  it('maps token_expired to the danger tone', () => {
    expect(STATUS_TONES.token_expired).toBe('danger');
  });

  it('exposes exactly the two persisted statuses as label keys', () => {
    expect([...STATUS_LABEL_KEYS].sort()).toEqual(['active', 'token_expired']);
  });

  it('has a tone for every label key', () => {
    for (const key of STATUS_LABEL_KEYS) {
      expect(STATUS_TONES[key as IntegrationStatus]).toBeDefined();
    }
  });
});
