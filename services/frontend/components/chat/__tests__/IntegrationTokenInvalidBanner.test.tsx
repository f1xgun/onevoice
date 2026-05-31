import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { IntegrationTokenInvalidBanner } from '../IntegrationTokenInvalidBanner';

declare const __setTestLocale: (locale: 'ru' | 'en') => void;

describe('IntegrationTokenInvalidBanner', () => {
  it('renders tokenTelegram summary copy for platform=telegram', () => {
    render(<IntegrationTokenInvalidBanner platform="telegram" />);
    expect(screen.getByText(/Telegram больше не принимает наш токен/i)).toBeInTheDocument();
  });

  it('renders tokenVk summary copy for platform=vk', () => {
    render(<IntegrationTokenInvalidBanner platform="vk" />);
    expect(screen.getByText(/доступ к сообществу ВКонтакте истёк/i)).toBeInTheDocument();
  });

  it('renders tokenGeneric summary copy for other platforms', () => {
    render(<IntegrationTokenInvalidBanner platform="yandex_business" />);
    expect(screen.getByText(/Доступ к платформе истёк/i)).toBeInTheDocument();
  });

  it('CTA href equals /integrations?reconnect={platform} for telegram', () => {
    render(<IntegrationTokenInvalidBanner platform="telegram" />);
    const link = screen.getByRole('link');
    expect(link).toHaveAttribute('href', '/integrations?reconnect=telegram');
  });

  it('CTA href equals /integrations?reconnect={platform} for vk', () => {
    render(<IntegrationTokenInvalidBanner platform="vk" />);
    const link = screen.getByRole('link');
    expect(link).toHaveAttribute('href', '/integrations?reconnect=vk');
  });

  it('RU: CTA label is "Переподключить Telegram" for telegram', () => {
    render(<IntegrationTokenInvalidBanner platform="telegram" />);
    expect(screen.getByRole('link', { name: 'Переподключить Telegram' })).toBeInTheDocument();
  });

  it('RU: CTA label is "Переподключить ВКонтакте" for vk', () => {
    render(<IntegrationTokenInvalidBanner platform="vk" />);
    expect(screen.getByRole('link', { name: 'Переподключить ВКонтакте' })).toBeInTheDocument();
  });

  it('EN: CTA label is "Reconnect Telegram" for telegram', () => {
    __setTestLocale('en');
    render(<IntegrationTokenInvalidBanner platform="telegram" />);
    expect(screen.getByRole('link', { name: 'Reconnect Telegram' })).toBeInTheDocument();
  });

  it('EN: CTA label is "Reconnect VK" for vk', () => {
    __setTestLocale('en');
    render(<IntegrationTokenInvalidBanner platform="vk" />);
    expect(screen.getByRole('link', { name: 'Reconnect VK' })).toBeInTheDocument();
  });

  it('root element declares role="alert" and aria-live="polite"', () => {
    render(<IntegrationTokenInvalidBanner platform="telegram" />);
    const alert = screen.getByRole('alert');
    expect(alert).toHaveAttribute('aria-live', 'polite');
  });
});
