export type RegistrationMode = 'open' | 'invite_only';

// Missing is open for local development and tests. Any explicit unknown value
// fails closed in the UI; the API rejects the same misconfiguration at boot.
export function parseRegistrationMode(value: string | undefined): RegistrationMode {
  if (value === undefined || value.trim() === '') return 'open';
  return value.trim().toLowerCase() === 'open' ? 'open' : 'invite_only';
}
