import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { WhitelistRadio } from '../WhitelistRadio';
import type { WhitelistMode } from '@/types/project';

describe('WhitelistRadio', () => {
  it('renders all four options with UI-SPEC labels and helper text', () => {
    render(<WhitelistRadio value="inherit" onChange={() => {}} />);

    expect(screen.getByText('Как в настройках организации')).toBeInTheDocument();
    expect(screen.getByText('Все инструменты')).toBeInTheDocument();
    expect(screen.getByText('Выбранные')).toBeInTheDocument();
    expect(screen.getByText('Никаких')).toBeInTheDocument();

    expect(
      screen.getByText(
        'Использовать настройки организации (по умолчанию доступны все инструменты).'
      )
    ).toBeInTheDocument();
    expect(
      screen.getByText('Любой инструмент активной интеграции доступен ИИ.')
    ).toBeInTheDocument();
    expect(screen.getByText('Разрешить только отмеченные ниже.')).toBeInTheDocument();
    expect(
      screen.getByText('ИИ может отвечать, но не будет выполнять действия.')
    ).toBeInTheDocument();
  });

  it.each<[string, WhitelistMode]>([
    ['Как в настройках организации', 'inherit'],
    ['Все инструменты', 'all'],
    ['Выбранные', 'explicit'],
    ['Никаких', 'none'],
  ])('clicking %s fires onChange with "%s"', async (label, expected) => {
    const onChange = vi.fn();
    const initial: WhitelistMode = expected === 'inherit' ? 'none' : 'inherit';
    render(<WhitelistRadio value={initial} onChange={onChange} />);

    const user = userEvent.setup();
    await user.click(screen.getByText(label));

    expect(onChange).toHaveBeenCalledWith(expected);
  });
});
