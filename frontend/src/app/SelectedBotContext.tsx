// SelectedBotContext carries the current conversation target (bot id) above
// the routes so the team list, the conversation shell and the work panel all
// agree on one selection. It intentionally stores only the bot id: the team
// is always derived from the freshest teams query, never cached here.
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';

interface SelectedBotContextValue {
  botId: string | null;
  setBotId: (botId: string | null) => void;
  drafts: Readonly<Record<string, string>>;
  setDraft: (botId: string, text: string) => void;
  clearSentDraft: (botId: string, sent: string) => void;
}

const SelectedBotContext = createContext<SelectedBotContextValue | null>(null);

// The selection survives a reload in this browser; a stored id whose member
// is gone is cleared by the layout once the teams load.
const storageKey = 'cobot.selectedBot';

function storedBotId(): string | null {
  try {
    return window.localStorage.getItem(storageKey);
  } catch {
    return null;
  }
}

export function SelectedBotProvider({ children }: { children: ReactNode }) {
  const [botId, setBotId] = useState<string | null>(storedBotId);
  useEffect(() => {
    try {
      if (botId) window.localStorage.setItem(storageKey, botId);
      else window.localStorage.removeItem(storageKey);
    } catch {
      /* storage unavailable: the selection just does not persist */
    }
  }, [botId]);
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const value = useMemo(() => ({ botId, setBotId, drafts,
    setDraft: (id: string, text: string) => setDrafts((current) => ({ ...current, [id]: text })),
    clearSentDraft: (id: string, sent: string) => setDrafts((current) => current[id] === sent ? { ...current, [id]: '' } : current),
  }), [botId, drafts]);
  return <SelectedBotContext.Provider value={value}>{children}</SelectedBotContext.Provider>;
}

export function useSelectedBot(): SelectedBotContextValue {
  const value = useContext(SelectedBotContext);
  if (!value) throw new Error('useSelectedBot must be used inside SelectedBotProvider');
  return value;
}
