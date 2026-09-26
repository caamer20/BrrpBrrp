// Trap focus only while a responsive sidebar acts as a modal drawer.
export function drawerFocus(panel: HTMLElement, close: () => void, trigger?: HTMLElement | null) {
  const previous = trigger ?? (document.activeElement as HTMLElement | null);
  const focusable = () =>
    Array.from(
      panel.querySelectorAll<HTMLElement>(
        'button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex="0"]'
      )
    ).filter((element) => element.getClientRects().length > 0 && !element.closest('[inert]'));
  focusable()[0]?.focus();
  const keydown = (event: KeyboardEvent) => {
    if (document.querySelector('dialog[open]')) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      close();
    }
    if (event.key !== 'Tab') return;
    const elements = focusable();
    const first = elements[0],
      last = elements.at(-1);
    if (!first) {
      event.preventDefault();
      return;
    }
    if (
      event.shiftKey &&
      (document.activeElement === first || !panel.contains(document.activeElement))
    ) {
      event.preventDefault();
      last?.focus();
    } else if (
      !event.shiftKey &&
      (document.activeElement === last || !panel.contains(document.activeElement))
    ) {
      event.preventDefault();
      first.focus();
    }
  };
  window.addEventListener('keydown', keydown);
  return () => {
    window.removeEventListener('keydown', keydown);
    // Svelte removes inert/visibility in the same update; restore after that flush.
    queueMicrotask(() => {
      if (previous?.isConnected && !document.querySelector('dialog[open]')) previous.focus();
    });
  };
}
