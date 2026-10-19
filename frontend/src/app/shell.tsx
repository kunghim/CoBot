// The workspace shell's state: whether the team list and the work panel are
// open, and whether the window is narrow. The panels always sit in the
// layout beside the conversation, never over it, so opening one never stops
// the user from working; on a narrow window opening one side closes the
// other, so the conversation keeps its room. The toggles live at the two
// ends of the desktop title bar when the app draws one, otherwise at the two
// ends of the page header — the conversation header, or the home page's top
// row — so they never move.
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
  /** A page of the work panel (the browser) asks for more room. */
  panelWide: boolean;
  setPanelWide: (wide: boolean) => void;
  /** The app draws its own title bar (desktop), which holds the toggles. */
  titleBar: boolean;
}

const ShellContext = createContext<Shell | null>(null);

const WIDE = '(min-width: 1024px)';

function isWide(): boolean {
  return typeof window === 'undefined' || typeof window.matchMedia !== 'function' || window.matchMedia(WIDE).matches;
}

export function ShellProvider({ children, titleBar = false }: { children: ReactNode; titleBar?: boolean }) {
  const [narrow, setNarrow] = useState(() => !isWide());
  // A wide window starts with both panels open; a narrow one with the team
  // list only, so the conversation is not squeezed on the first load.
  const [listOpen, setList] = useState(true);
  const [panelOpen, setPanel] = useState(isWide);
  const [panelWide, setPanelWide] = useState(false);
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return;
    const query = window.matchMedia(WIDE);
    const update = () => {
      setNarrow(!query.matches);
      // Becoming narrow with both open keeps the list and folds the panel.
      if (!query.matches) setPanel(false);
    };
    query.addEventListener('change', update);
    return () => query.removeEventListener('change', update);
  }, []);
  // On a narrow window the two sides take turns.
  const setListOpen = useCallback((open: boolean) => {
    setList(open);
    if (open && !isWide()) setPanel(false);
  }, []);
  const setPanelOpen = useCallback((open: boolean) => {
    setPanel(open);
    if (open && !isWide()) setList(false);
  }, []);
  // Showing something in the work panel opens it first.
  useWorkPanelRequests(useCallback(() => setPanelOpen(true), [setPanelOpen]));
  return <ShellContext.Provider value={{ listOpen, panelOpen, narrow, setListOpen, setPanelOpen, panelWide, setPanelWide, titleBar }}>{children}</ShellContext.Provider>;
}

export function useShell(): Shell {
  const shell = useContext(ShellContext);
  if (!shell) throw new Error('useShell must be used inside ShellProvider');
  return shell;
}

/**
 * The toggle for one side panel. A page header places it with place
 * 'header'; it then steps aside when the title bar holds the toggles.
 */
export function ShellToggle({ side, place = 'header' }: { side: 'left' | 'right'; place?: 'header' | 'titlebar' }) {
  const { t } = useTranslation();
  const shell = useShell();
  if (place === 'header' && shell.titleBar) return null;
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
