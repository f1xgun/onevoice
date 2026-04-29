import { describe, expect, it, vi, beforeEach } from 'vitest';

const errorMock = vi.fn();
const warningMock = vi.fn();

vi.mock('sonner', () => ({
  toast: {
    error: errorMock,
    warning: warningMock,
  },
}));

describe('lib/toast helpers', () => {
  beforeEach(() => {
    errorMock.mockClear();
    warningMock.mockClear();
  });

  it('errorWithAction forwards the action and description to sonner.error', async () => {
    const { errorWithAction } = await import('../toast');
    const onClick = vi.fn();

    errorWithAction(
      'Не получилось отправить',
      'Разбить и отправить',
      onClick,
      { description: 'Telegram отклонил сообщение — слишком длинное.' }
    );

    expect(errorMock).toHaveBeenCalledTimes(1);
    const [message, opts] = errorMock.mock.calls[0];
    expect(message).toBe('Не получилось отправить');
    expect(opts.description).toBe('Telegram отклонил сообщение — слишком длинное.');
    expect(opts.action.label).toBe('Разбить и отправить');

    opts.action.onClick();
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('warningWithAction forwards to sonner.warning', async () => {
    const { warningWithAction } = await import('../toast');
    const onClick = vi.fn();

    warningWithAction('VK потерял авторизацию', 'Переподключить', onClick);

    expect(warningMock).toHaveBeenCalledTimes(1);
    const [message, opts] = warningMock.mock.calls[0];
    expect(message).toBe('VK потерял авторизацию');
    expect(opts.action.label).toBe('Переподключить');
  });
});
