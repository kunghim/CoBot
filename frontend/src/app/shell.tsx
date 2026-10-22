// The workspace shell's state: whether the team list is open, and whether
// the window is narrow. The list always sits in the layout beside the
// conversation, never over it; on a narrow window it steps aside so the
// conversation keeps its room. The toggle lives at the left end of the
// desktop title bar when the app draws one, otherwise at the left end of the
// page header, so it never moves. The workspace on the right and the info
// card over the conversation have their own state (features/workspace).
import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { PanelToggle } from '../components/ui';

interface Shell {
  listOpen: boolean;
  narrow: boolean;
  setListOpen: (open: boolean) => void;
  /** The app draws its own title bar (desktop), which holds the toggle. */
  titleBar: boolean;
}

const ShellContext = createContext<Shell | null>(null);

const WIDE = '(min-width: 1024px)';

function isWide(): boolean {
  return typeof window === 'undefined' || typeof window.matchMedia !== 'function' || window.matchMedia(WIDE).matches;
}

export function ShellProvider({ children, titleBar = false }: { children: ReactNode; titleBar?: boolean }) {
  const [narrow, setNarrow] = useState(() => !isWide());
  // The list starts open at every width: on a narrow window it is the only
  // side beside the conversation, and folding it is one click away.
  const [listOpen, setListOpenState] = useState(true);
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return;
    const query = window.matchMedia(WIDE);
    const update = () => setNarrow(!query.matches);
    query.addEventListener('change', update);
    return () => query.removeEventListener('change', update);
  }, []);
  const setListOpen = useCallback((open: boolean) => setListOpenState(open), []);
  return <ShellContext.Provider value={{ listOpen, narrow, setListOpen, titleBar }}>{children}</ShellContext.Provider>;
}

export function useShell(): Shell {
  const shell = useContext(ShellContext);
  if (!shell) throw new Error('useShell must be used inside ShellProvider');
  return shell;
}

/**
 * The toggle for the team list. A page header places it with place
 * 'header'; it then steps aside when the title bar holds the toggle.
 */
export function ShellToggle({ place = 'header' }: { place?: 'header' | 'titlebar' }) {
  const { t } = useTranslation();
  const shell = useShell();
  if (place === 'header' && shell.titleBar) return null;
  return (
    <PanelToggle
      side="left"
      open={shell.listOpen}
      onToggle={() => shell.setListOpen(!shell.listOpen)}
      openLabel={t('nav.openTeamList')}
      closeLabel={t('nav.closeTeamList')}
    />
  );
}
