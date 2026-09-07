import { useState } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import {
  AppDialog,
  Dialog,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from './AppDialog';
import { SheetContent } from './AppSheet';
import { ActionButton } from './ActionButton';
import { hasLayoutBrowser, withLayoutPage } from '@/test-utils/browser-layout';

function Fixture({ sheet = false }: { sheet?: boolean }) {
  const Surface = sheet ? SheetContent : AppDialog;
  const [open, setOpen] = useState(false);
  return (
    <>
      <ActionButton onClick={() => setOpen(true)}>Open</ActionButton>
      <Dialog open={open} onOpenChange={setOpen}>
        <Surface>
          <DialogHeader>
            <DialogTitle>Connection</DialogTitle>
            <DialogDescription>Enter account details</DialogDescription>
          </DialogHeader>
          <form>
            <label htmlFor="account">Account</label>
            <input id="account" />
            <p>{'Long description '.repeat(150)}</p>
            <ActionButton>Connect</ActionButton>
          </form>
          <DialogFooter>
            <ActionButton onClick={() => setOpen(false)}>Cancel</ActionButton>
          </DialogFooter>
        </Surface>
      </Dialog>
    </>
  );
}

describe('AppDialog', () => {
  it.each([false, true])(
    'traps keyboard focus, closes on Escape and restores a programmatic opener (sheet: %s)',
    async (sheet) => {
      const user = userEvent.setup();
      render(<Fixture sheet={sheet} />);
      const opener = screen.getByRole('button', { name: 'Open' });
      await user.click(opener);
      const dialog = screen.getByRole('dialog');
      expect(dialog).toHaveAccessibleName('Connection');
      expect(dialog).toHaveAccessibleDescription('Enter account details');
      for (let index = 0; index < 8; index++) {
        await user.tab();
        expect(dialog.contains(document.activeElement)).toBe(true);
      }
      await user.keyboard('{Escape}');
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
      expect(opener).toHaveFocus();
    }
  );

  it.skipIf(!hasLayoutBrowser).each([320, 375, 1440])(
    'keeps close and final action reachable at %i px with a reduced keyboard viewport',
    async (width) => {
      render(<Fixture />);
      await userEvent.click(screen.getByRole('button', { name: 'Open' }));
      await withLayoutPage(document.body.innerHTML, { width, height: 320 }, async (page) => {
        const dialog = page.getByRole('dialog');
        const box = (await dialog.boundingBox())!;
        expect(box.x).toBeGreaterThanOrEqual(16);
        expect(box.x + box.width).toBeLessThanOrEqual(width - 16);
        expect(box.height).toBeLessThanOrEqual(288);
        const close = await page
          .getByRole('button', { name: 'Закрыть', exact: true })
          .boundingBox();
        expect(close!.width).toBeGreaterThanOrEqual(44);
        expect(close!.height).toBeGreaterThanOrEqual(44);
        const submit = page.getByRole('button', { name: 'Connect', exact: true });
        await submit.focus();
        const action = (await submit.boundingBox())!;
        expect(action.y).toBeGreaterThanOrEqual(box.y);
        expect(action.y + action.height).toBeLessThanOrEqual(box.y + box.height);
      });
    },
    15000
  );
});
