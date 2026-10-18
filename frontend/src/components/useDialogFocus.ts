import { useLayoutEffect, useRef } from 'react';

const dialogs: HTMLElement[] = [];
const selector = 'a[href], button, input, select, textarea, [tabindex], [contenteditable="true"]';

function controls(dialog: HTMLElement) {
  return Array.from(dialog.querySelectorAll<HTMLElement>(selector)).filter((element) => {
    if (element.tabIndex < 0 || element.matches(':disabled, input[type="hidden"]')) return false;
    for (let node: HTMLElement | null = element; node; node = node.parentElement) {
      const style = getComputedStyle(node);
      if (node.hidden || node.hasAttribute('inert') || style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') return false;
    }
    return true;
  });
}

export function useDialogFocus(open: boolean, onClose: () => void, closeDisabled = false) {
  const dialogRef = useRef<HTMLElement>(null);
  const current = useRef({ onClose, closeDisabled });
  useLayoutEffect(() => { current.current = { onClose, closeDisabled }; });

  useLayoutEffect(() => {
    const dialog = dialogRef.current;
    if (!open || !dialog) return;
    const trigger = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    // Parent layout effects can run after their children; keep nested dialogs on top.
    const childIndex = dialogs.findIndex((item) => dialog.contains(item));
    if (childIndex < 0) dialogs.push(dialog);
    else dialogs.splice(childIndex, 0, dialog);
    // Capture the trigger before applying initial focus; callers must not use autoFocus.
    const initial = controls(dialog);
    if (dialogs[dialogs.length - 1] === dialog) {
      (initial.find((element) => element.matches('input:not([type="checkbox"]):not([type="radio"]), select, textarea')) ?? initial[0] ?? dialog).focus();
    }
    const onKey = (event: KeyboardEvent) => {
      if (dialogs[dialogs.length - 1] !== dialog || event.defaultPrevented) return;
      if (event.key === 'Escape') {
        event.preventDefault();
        event.stopImmediatePropagation();
        if (!current.current.closeDisabled) current.current.onClose();
      } else if (event.key === 'Tab') {
        const items = controls(dialog);
        const index = items.indexOf(document.activeElement as HTMLElement);
        event.preventDefault();
        const next = index < 0 ? (event.shiftKey ? items.length - 1 : 0) : (index + (event.shiftKey ? -1 : 1) + items.length) % items.length;
        (items[next] ?? dialog).focus();
      }
    };
    document.addEventListener('keydown', onKey, true);
    return () => {
      document.removeEventListener('keydown', onKey, true);
      const wasCurrent = dialogs[dialogs.length - 1] === dialog;
      dialogs.splice(dialogs.indexOf(dialog), 1);
      if (wasCurrent && trigger?.isConnected) trigger.focus();
    };
  }, [open]);
  return dialogRef;
}
