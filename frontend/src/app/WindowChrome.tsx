// The header rows that replaced the old full-width title bar. The columns
// reach the top of the window: the sidebar's head carries the search field
// and the mode capsule, the conversation's head carries the list toggle and
// (on Windows) the window controls, and on macOS while windowed the corner
// reservation is the one place the system traffic lights land — a single
// side-colored patch over the rail and the sidebar, gone in fullscreen.
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useLocation } from 'react-router-dom';
import { ConversationHead, SidebarHead, type WindowControls } from '../components/ui';
import { useWindowFrame } from '../platform/desktopWindow';
import ModeSwitch from './ModeSwitch';
import SearchBox from '../features/search/SearchBox';
import { ShellToggle, useShell } from './shell';

/** True only on the macOS desktop while windowed: the corner reservation exists. */
export function useMacWindowed(): boolean {
  const window = useWindowFrame();
  return window?.platform === 'mac' && !window.fullscreen;
}

/** The traffic-light reservation: 88px of the sidebar surface at the top-left
 * corner. Nothing interactive lives under it. */
export function WindowCorner() {
  const macWindowed = useMacWindowed();
  if (!macWindowed) return null;
  return <div data-testid="native-titlebar-inset" aria-hidden className="absolute left-0 top-0 z-10 h-10 w-[88px] bg-side" style={{ ['--wails-draggable' as string]: 'drag' }} />;
}

/** The sidebar's head: conversation search at the left end, mode capsule at
 * the right end, clear of the corner reservation while windowed. */
export function SidebarHeader() {
  const path = useLocation().pathname;
  return (
    <SidebarHead inset={useMacWindowed()} leading={<SearchBox />}>
      <ModeSwitch value={path.startsWith('/chat') ? 'chat' : 'work'} />
    </SidebarHead>
  );
}

/** The conversation's head: the list toggle flush with the sheet below, an
 * optional title, and the window controls on Windows. While macOS is
 * windowed and the sidebar is folded (or absent), the conversation's left
 * edge slides under the corner reservation, so the head clears it. */
export function ConversationHeader({ title, right, toggle = true, alwaysInset = false }: { title?: ReactNode; right?: ReactNode; toggle?: boolean; alwaysInset?: boolean }) {
  const { t } = useTranslation();
  const desktop = useWindowFrame();
  const { listVisible } = useShell();
  const controls: WindowControls | undefined = desktop ? {
    minimizeLabel: t('window.minimize'),
    maximizeLabel: t('window.maximize'),
    restoreLabel: t('window.restore'),
    closeLabel: t('window.close'),
    maximized: desktop.maximized,
    onMinimize: desktop.minimize,
    onToggleMaximize: desktop.toggleMaximize,
    onClose: desktop.close,
  } : undefined;
  const inset = useMacWindowed() && (alwaysInset || !listVisible);
  return <ConversationHead inset={inset} left={toggle ? <ShellToggle place="titlebar" /> : undefined} title={title} right={right} windowControls={desktop?.platform === 'windows' ? controls : undefined} />;
}
