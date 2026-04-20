import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { WhitelistRadio } from '../WhitelistRadio';
import type { WhitelistMode } from '@/types/project';

describe('WhitelistRadio', () => {
  it('renders all four options with UI-SPEC labels and helper text', () => {
    render(<WhitelistRadio value="inherit" onChange={() => {}} />);

    // Labels
    expect(screen.getByText('Как у бизнеса')).toBeInTheDocument();
    expect(screen.getByText('Все инструменты')).toBeInTheDocument();
    expect(screen.getByText('Выбранные')).toBeInTheDocument();
    expect(screen.getByText('Никаких')).toBeInTheDocument();

    // Helper text
    expect(
      screen.getByText('Использовать настройки бизнеса (по умолчанию все доступные инструменты).')
    ).toBeInTheDocument();
    expect(
      screen.getByText('Любой инструмент активной интеграции доступен LLM.')
    ).toBeInTheDocument();
    expect(screen.getByText('Разрешить только отмеченные ниже.')).toBeInTheDocument();
    expect(
      screen.getByText('LLM может отвечать, но не будет выполнять действия.')
    ).toBeInTheDocument();
  });

  it.each<[string, WhitelistMode]>([
    ['Как у бизнеса', 'inherit'],
    ['Все инструменты', 'all'],
    ['Выбранные', 'explicit'],
    ['Никаких', 'none'],
  ])('clicking %s fires onChange with "%s"', async (label, expected) => {
    const onChange = vi.fn();
    // Start on a value different from `expected` so the click always produces
    // a state change (Radix suppresses onValueChange when selecting the
    // already-selected value).
    const initial: WhitelistMode = expected === 'inherit' ? 'none' : 'inherit';
    render(<WhitelistRadio value={initial} onChange={onChange} />);

    const user = userEvent.setup();
    await user.click(screen.getByText(label));

    expect(onChange).toHaveBeenCalledWith(expected);
  });
});
