// The composer's model picker opens from where the user is: switching from
// its own trigger, or from a failed turn's switch-model entry. A window
// event bridges the two places, so the picker stays the single model
// chooser and the failure row does not grow its own.
import { useEffect } from 'react';

const OPEN_EVENT = 'cobot:open-model-picker';

export function openModelPicker(botId: string): void {
  window.dispatchEvent(new CustomEvent(OPEN_EVENT, { detail: { botId } }));
}

/** Opens the picker when something asks for it: the given member's picker
 * reacts to its own requests only. */
export function useOpenModelPicker(botId: string, onOpen: () => void): void {
  useEffect(() => {
    const handler = (event: Event) => {
      if ((event as CustomEvent<{ botId?: string }>).detail?.botId === botId) onOpen();
    };
    window.addEventListener(OPEN_EVENT, handler);
    return () => window.removeEventListener(OPEN_EVENT, handler);
  }, [botId, onOpen]);
}
