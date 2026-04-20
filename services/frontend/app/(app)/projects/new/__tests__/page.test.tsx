import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render } from '@testing-library/react';
import { toast } from 'sonner';
import NewProjectPage from '../page';

const push = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push, back: vi.fn(), replace: vi.fn() }),
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// Stub out ProjectForm so we can invoke its onSaved prop directly without
// going through the full form lifecycle (react-hook-form, zod, API calls).
let capturedOnSaved: ((saved: { id: string; name: string }) => void) | null = null;
vi.mock('@/components/projects/ProjectForm', () => ({
  ProjectForm: (props: { onSaved: (saved: { id: string; name: string }) => void }) => {
    capturedOnSaved = props.onSaved;
    return null;
  },
}));

describe('NewProjectPage — post-create behaviour (GAP-04)', () => {
  beforeEach(() => {
    push.mockReset();
    (toast.success as ReturnType<typeof vi.fn>).mockReset();
    capturedOnSaved = null;
  });

  it('redirects to /projects/:id (edit page) after successful create so the user can configure', () => {
    render(<NewProjectPage />);
    expect(capturedOnSaved).toBeTypeOf('function');
    capturedOnSaved!({ id: 'p1', name: 'Отзывы' });
    expect(push).toHaveBeenCalledWith('/projects/p1');
    expect(push).toHaveBeenCalledTimes(1);
  });

  it('shows toast with project name', () => {
    render(<NewProjectPage />);
    capturedOnSaved!({ id: 'p1', name: 'Отзывы' });
    const calls = (toast.success as ReturnType<typeof vi.fn>).mock.calls;
    expect(calls).toHaveLength(1);
    expect(calls[0][0]).toContain('Отзывы');
    expect(calls[0][0]).toContain('создан');
  });

  it('does NOT redirect to Phase 19 placeholder /projects/:id/chats (regression guard)', () => {
    render(<NewProjectPage />);
    capturedOnSaved!({ id: 'p1', name: 'Отзывы' });
    expect(push).not.toHaveBeenCalledWith(expect.stringMatching(/\/projects\/.*\/chats/));
  });
});
