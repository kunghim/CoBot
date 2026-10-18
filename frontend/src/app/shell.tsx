// The workspace shell's state: whether the team list and the work panel are
// open, and whether the window is narrow (panels then slide over the
// conversation). The toggles live at the two ends of the page header —
// the conversation header, or the home page's top row — so they never move.
import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { PanelToggle } from '../components/ui';
import { useWorkPanelRequests } from './workPanelNav';

interface Shell {
  listOpen: boolean;
  panelOpen: boolean;
  narrow: boolean;
  setListOpen: (open: boolean) => void;
  setPanelOpen: (open: boolean) => void;
}

const ShellContext = createContext<Shell | null>(null);

const WIDE = '(min-width: 1024px)';

function isWide(): boolean {
  return typeof window === 'undefined' || typeof window.matchMedia !== 'function' || window.matchMedia(WIDE).matches;
}

export function ShellProvider({ children }: { children: ReactNode }) {
  const [narrow, setNarrow] = useState(() => !isWide());
  // Narrow windows start with both panels closed: a first load shows the
  // conversation, not a panel covering it.
  const [listOpen, setListOpen] = useState(isWide);
  const [panelOpen, setPanelOpen] = useState(isWide);
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return;
    const query = window.matchMedia(WIDE);
    const update = () => {
      setNarrow(!query.matches);
      if (!query.matches) {
        setListOpen(false);
        setPanelOpen(false);
      }
    };
    query.addEventListener('change', update);
    return () => query.removeEventListener('change', update);
  }, []);
  // Showing something in the work panel opens it first.
  useWorkPanelRequests(useCallback(() => setPanelOpen(true), []));
  return <ShellContext.Provider value={{ listOpen, panelOpen, narrow, setListOpen, setPanelOpen }}>{children}</ShellContext.Provider>;
}

export function useShell(): Shell {
  const shell = useContext(ShellContext);
  if (!shell) throw new Error('useShell must be used inside ShellProvider');
  return shell;
}

/** The toggle for one side panel, for the two ends of a page header. */
export function ShellToggle({ side }: { side: 'left' | 'right' }) {
  const { t } = useTranslation();
  const shell = useShell();
  const open = side === 'left' ? shell.listOpen : shell.panelOpen;
  return (
    <PanelToggle
      side={side}
      open={open}
      onToggle={() => (side === 'left' ? shell.setListOpen(!open) : shell.setPanelOpen(!open))}
      openLabel={t(side === 'left' ? 'nav.openTeamList' : 'nav.openWorkPanel')}
      closeLabel={t(side === 'left' ? 'nav.closeTeamList' : 'nav.closeWorkPanel')}
    />
  );
}
