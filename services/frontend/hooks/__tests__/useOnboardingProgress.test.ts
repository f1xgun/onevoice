import { describe, it, expect } from 'vitest';

import { deriveOnboarding, type OnboardingSignals } from '@/hooks/useOnboardingProgress';

// Fully-settled signals with nothing done. Individual tests flip one flag at a
// time to prove each step's done-derivation is independent + correctly sourced.
function baseSignals(over: Partial<OnboardingSignals> = {}): OnboardingSignals {
  return {
    hasBusiness: false,
    businessSettled: true,
    hasActiveIntegration: false,
    integrationsSettled: true,
    hasDescription: false,
    profileSettled: true,
    hasFirstAction: false,
    conversationsSettled: true,
    showInvite: false,
    hasTeammate: false,
    ...over,
  };
}

function stepById(signals: OnboardingSignals, id: string) {
  return deriveOnboarding(signals).steps.find((s) => s.id === id);
}

describe('deriveOnboarding — per-step done-signal derivation', () => {
  it('createOrg is done from hasBusiness (useBusinessList length ≥ 1)', () => {
    expect(stepById(baseSignals({ hasBusiness: true }), 'createOrg')?.done).toBe(true);
    expect(stepById(baseSignals({ hasBusiness: false }), 'createOrg')?.done).toBe(false);
  });

  it('connectChannel is done from hasActiveIntegration (integrations query)', () => {
    expect(stepById(baseSignals({ hasActiveIntegration: true }), 'connectChannel')?.done).toBe(
      true
    );
    expect(stepById(baseSignals({ hasActiveIntegration: false }), 'connectChannel')?.done).toBe(
      false
    );
  });

  it('describeOrg is done from hasDescription (business profile query)', () => {
    expect(stepById(baseSignals({ hasDescription: true }), 'describeOrg')?.done).toBe(true);
    expect(stepById(baseSignals({ hasDescription: false }), 'describeOrg')?.done).toBe(false);
  });

  it('firstAction is done from hasFirstAction (conversations lastMessageAt)', () => {
    expect(stepById(baseSignals({ hasFirstAction: true }), 'firstAction')?.done).toBe(true);
    expect(stepById(baseSignals({ hasFirstAction: false }), 'firstAction')?.done).toBe(false);
  });

  it('deep-links each step at the right in-app route', () => {
    const s = deriveOnboarding(baseSignals({ showInvite: true }));
    expect(s.steps.find((x) => x.id === 'connectChannel')?.href).toBe('/integrations');
    expect(s.steps.find((x) => x.id === 'describeOrg')?.href).toBe('/business');
    expect(s.steps.find((x) => x.id === 'firstAction')?.href).toBe('/chat');
    expect(s.steps.find((x) => x.id === 'inviteTeam')?.href).toBe('/settings/team');
  });
});

describe('deriveOnboarding — progress + allDone (gating steps only)', () => {
  it('counts only gating steps; invite never affects total/allDone', () => {
    const withInvite = deriveOnboarding(baseSignals({ showInvite: true, hasBusiness: true }));
    expect(withInvite.total).toBe(4);
    expect(withInvite.completedCount).toBe(1);
    expect(withInvite.steps.some((s) => s.id === 'inviteTeam')).toBe(true);
    expect(withInvite.steps.find((s) => s.id === 'inviteTeam')?.gating).toBe(false);

    const withoutInvite = deriveOnboarding(baseSignals({ hasBusiness: true }));
    expect(withoutInvite.total).toBe(4);
    expect(withoutInvite.steps.some((s) => s.id === 'inviteTeam')).toBe(false);
  });

  it('allDone only when every gating step is done', () => {
    const all = deriveOnboarding(
      baseSignals({
        hasBusiness: true,
        hasActiveIntegration: true,
        hasDescription: true,
        hasFirstAction: true,
      })
    );
    expect(all.completedCount).toBe(4);
    expect(all.allDone).toBe(true);

    const partial = deriveOnboarding(
      baseSignals({ hasBusiness: true, hasActiveIntegration: true, hasDescription: true })
    );
    expect(partial.completedCount).toBe(3);
    expect(partial.allDone).toBe(false);
  });

  it('inviteTeam done reflects hasTeammate without changing progress', () => {
    const s = deriveOnboarding(
      baseSignals({
        showInvite: true,
        hasTeammate: true,
        hasBusiness: true,
        hasActiveIntegration: true,
        hasDescription: true,
        hasFirstAction: true,
      })
    );
    expect(s.steps.find((x) => x.id === 'inviteTeam')?.done).toBe(true);
    // allDone is driven by the 4 gating steps; the invite checkmark is bonus.
    expect(s.allDone).toBe(true);
    expect(s.total).toBe(4);
  });
});

describe('deriveOnboarding — loading vs settled (isPlaceholderData trap)', () => {
  it('an unsettled query marks its step loading and blocks allDone', () => {
    const s = deriveOnboarding(
      baseSignals({
        hasBusiness: true,
        hasActiveIntegration: true,
        hasDescription: true,
        hasFirstAction: true,
        conversationsSettled: false,
      })
    );
    expect(s.steps.find((x) => x.id === 'firstAction')?.loading).toBe(true);
    expect(s.loaded).toBe(false);
    expect(s.allDone).toBe(false);
  });

  it('a settled-but-not-done query (e.g. errored/empty) is neither loading nor done', () => {
    // integrationsSettled true + hasActiveIntegration false models a resolved
    // query with no active channel — the step is actionable, not a spinner.
    const step = stepById(
      baseSignals({ integrationsSettled: true, hasActiveIntegration: false }),
      'connectChannel'
    );
    expect(step?.loading).toBe(false);
    expect(step?.done).toBe(false);
  });
});
