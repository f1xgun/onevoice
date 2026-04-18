import { describe, expect, it, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { ProjectForm } from '../ProjectForm';
import type { Project } from '@/types/project';

// Mock next/navigation to avoid router context.
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), back: vi.fn(), replace: vi.fn() }),
}));

// Mock sonner toast.
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// Mock axios-based API client: integrations endpoint + project CRUD.
vi.mock('@/lib/api', () => ({
  api: {
    get: vi.fn((url: string) => {
      if (url === '/integrations') return Promise.resolve({ data: [] });
      return Promise.resolve({ data: null });
    }),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}));

function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function Wrapper({ children }: { children: ReactNode }) {
  const client = makeClient();
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function renderForm(project?: Project) {
  return render(
    <Wrapper>
      <ProjectForm project={project} onSaved={() => {}} />
    </Wrapper>
  );
}

const sampleProject: Project = {
  id: 'p-1',
  businessId: 'b-1',
  name: 'Reviews',
  description: 'reply to reviews',
  systemPrompt: 'Always reply politely.',
  whitelistMode: 'explicit',
  allowedTools: ['telegram__send_channel_post'],
  quickActions: ['Проверить отзывы'],
  createdAt: '2026-04-18T00:00:00Z',
  updatedAt: '2026-04-18T00:00:00Z',
};

describe('ProjectForm', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders empty fields in create mode without a delete button', () => {
    renderForm();

    expect(screen.getByLabelText('Название')).toHaveValue('');
    expect(screen.getByRole('button', { name: 'Создать проект' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Удалить проект' })).not.toBeInTheDocument();
  });

  it('renders pre-filled fields in edit mode with a delete button', () => {
    renderForm(sampleProject);

    expect(screen.getByLabelText('Название')).toHaveValue('Reviews');
    expect(screen.getByRole('button', { name: 'Сохранить' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Удалить проект' })).toBeInTheDocument();
  });

  it('shows name error when submitting with empty name', async () => {
    renderForm();
    const user = userEvent.setup();

    await user.click(screen.getByRole('button', { name: 'Создать проект' }));

    expect(await screen.findByText('Укажите название проекта.')).toBeInTheDocument();
  });

  it('shows empty-explicit-whitelist error on submit with no tools', async () => {
    renderForm();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText('Название'), 'My Project');
    await user.click(screen.getByText('Выбранные'));
    await user.click(screen.getByRole('button', { name: 'Создать проект' }));

    expect(
      await screen.findByText(
        'Выберите хотя бы один инструмент или переключите режим на «Никаких».'
      )
    ).toBeInTheDocument();
  });

  it('shows long system-prompt error with exact copy', async () => {
    renderForm();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText('Название'), 'My Project');

    const textarea = screen.getByLabelText('Системный промпт') as HTMLTextAreaElement;
    const tooLong = 'a'.repeat(4001);
    // fireEvent.change uses React's native-input-value-setter shim so
    // react-hook-form's onChange handler receives the new value.
    fireEvent.change(textarea, { target: { value: tooLong } });

    await user.click(screen.getByRole('button', { name: 'Создать проект' }));

    await waitFor(() => {
      expect(
        screen.getByText('Системный промпт слишком длинный (максимум 4000 символов).')
      ).toBeInTheDocument();
    });
  });
});
