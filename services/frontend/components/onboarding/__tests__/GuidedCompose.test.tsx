import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { GuidedCompose } from '@/components/onboarding/GuidedCompose';

vi.mock('@/lib/telemetry', () => ({
  trackEvent: vi.fn(),
}));

describe('GuidedCompose — seeds the existing chat send path', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('calls onCompose with the composed announcement instruction on submit', async () => {
    const onCompose = vi.fn();
    const user = userEvent.setup();
    render(<GuidedCompose onCompose={onCompose} />);

    await user.click(screen.getByRole('button', { name: 'Составить пост' }));

    const topic = screen.getByLabelText('О чём пост');
    await user.type(topic, 'скидка 20% в эти выходные');

    await user.click(screen.getByRole('button', { name: 'Подготовить в чате' }));

    expect(onCompose).toHaveBeenCalledTimes(1);
    expect(onCompose).toHaveBeenCalledWith(
      'Напиши анонс для организации на тему: скидка 20% в эти выходные. Составь готовый пост.'
    );
  });

  it('keeps submit inert (no onCompose) while the topic is blank', async () => {
    const onCompose = vi.fn();
    const user = userEvent.setup();
    render(<GuidedCompose onCompose={onCompose} />);

    await user.click(screen.getByRole('button', { name: 'Составить пост' }));

    const submit = screen.getByRole('button', { name: 'Подготовить в чате' });
    expect(submit).toBeDisabled();

    await user.click(submit);
    expect(onCompose).not.toHaveBeenCalled();
  });

  it('does not seed sendMessage when disabled (a stream is in flight)', async () => {
    const onCompose = vi.fn();
    const user = userEvent.setup();
    render(<GuidedCompose onCompose={onCompose} disabled />);

    const trigger = screen.getByRole('button', { name: 'Составить пост' });
    expect(trigger).toBeDisabled();
    await user.click(trigger);

    expect(onCompose).not.toHaveBeenCalled();
  });
});
