// The header rows that replaced the old full-width title bar. The columns
// reach the top of the window: the sidebar's head is the lights' strip while
// windowed (the list toggle lives at the conversation's head), the search
// field and the mode capsule sit in their own row inside the secondary menu
// below it, and on macOS while windowed the corner reservation is the one
// place the system traffic lights land — a single side-colored patch over
// the rail and the sidebar, gone in fullscreen.
import type { ReactNode } from 'react';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { ConversationHead, type WindowControls, cx } from '../components/ui';
import { useWindowFrame } from '../platform/desktopWindow';
import ModeSwitch from './ModeSwitch';
import SearchBox from '../features/search/SearchBox';
import { SidebarToggle } from './SidebarToggle';
import { ShellToggle, useShell } from './shell';
import { useLocation } from 'react-router-dom';

/** Names the window while fullscreen: macOS reveals the transparent titlebar
 * under the hover menu bar and draws this title beside the traffic lights
 * (Codex-style), so the strip reads as a title bar instead of dead space.
 * Windowed keeps the title empty — the columns carry their own headers. */
export function useNativeTitle(title: string | undefined) {
  const desktop = useWindowFrame();
  const fullscreen = desktop?.fullscreen ?? false;
  useEffect(() => {
    desktop?.setNativeTitle(fullscreen ? title ?? '' : '');
    return () => desktop?.setNativeTitle('');
  }, [desktop, fullscreen, title]);
}

/** True on the macOS desktop: the head rows use the titlebar's 28px metrics
 * in every window state so the controls hold one position across the
 * fullscreen boundary. */
export function useMacCorner(): boolean {
  return useWindowFrame()?.platform === 'mac';
}

/** True only on the macOS desktop while windowed: the traffic lights sit in
 * the page's top-left corner, so the layout reserves it. Fullscreen cancels
 * the reservation — the hover menu bar overlays the content like any macOS
 * app, and the top of the window stays fully used. */
export function useMacWindowed(): boolean {
  const window = useWindowFrame();
  return window?.platform === 'mac' && !window.fullscreen;
}

/** The traffic-light reservation block: 88x28 of the sidebar surface at
 * the top-left corner while windowed — the slim head row's own extent, so
 * the block fills exactly the strip the lights occupy. Nothing interactive
 * lives under it. */
export function WindowCorner() {
  const macWindowed = useMacWindowed();
  if (!macWindowed) return null;
  return <div data-testid="native-titlebar-inset" aria-hidden className="absolute left-0 top-0 z-10 h-7 w-[88px] bg-side" style={{ ['--wails-draggable' as string]: 'drag' }} />;
}

/** The sidebar's head: the window-top row above the secondary menu. The
 * list toggle lives here at a fixed spot — beside the traffic lights while
 * windowed, at the row's left edge in fullscreen — never moving with the
 * menu or the window state. */
/** True on the mac desktop: the fixed top-row toggle renders here — beside
 * the lights while windowed, the row's left edge in fullscreen — surviving
 * the menu's fold, which only unmounts the menu column below it. */
export function SidebarHeader() {
  const { t } = useTranslation();
  const shell = useShell();
  const chat = useLocation().pathname.startsWith('/chat');
  const macWindowed = useMacWindowed();
  if (!useMacCorner()) return null;
  const labels = {
    openLabel: t(chat ? 'chat.expandHistory' : 'nav.openTeamList'),
    closeLabel: t(chat ? 'chat.collapseHistory' : 'nav.closeTeamList'),
  };
  // Absolutely positioned over the window's top-left: it never takes flex
  // width, so narrow windows cannot overflow, and it survives the menu's
  // fold because it is not a child of the menu column.
  return (
    <div
      data-testid="top-row-toggle"
      className={cx('absolute left-0 top-0 z-20 flex h-7 items-center', macWindowed && 'pl-[88px]')}
      style={{ ['--wails-draggable' as string]: 'no-drag' }}
      onDoubleClick={(event) => event.stopPropagation()}
    >
      <SidebarToggle open={shell.listVisible} onToggle={shell.toggleList} {...labels} />
    </div>
  );
}

/** The secondary menu's tools row, inside the menu column above the list
 * title: search at the left end, mode capsule at the right end. It follows
 * the menu (hidden when the menu folds) and never shares a row with the
 * system lights. */
export function MenuTools({ value }: { value: 'chat' | 'work' }) {
  return (
    <div className={cx('flex shrink-0 items-center bg-side', useMacCorner() ? 'h-10 px-2 pb-1' : 'px-2 py-2')}>
      <div className="flex h-full min-w-0 flex-1 items-center justify-between gap-2">
        <SearchBox />
        <ModeSwitch value={value} />
      </div>
    </div>
  );
}

/** The conversation's head: the list toggle flush with the sheet below, an
 * optional title, and the window controls on Windows. While macOS is
 * windowed and the sidebar is folded (or absent), the conversation's left
 * edge slides under the traffic lights, so the head clears it. */
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
  // Web and Windows keep the toggle at the conversation's head; the mac
  // desktop's fixed top-row spot renders in SidebarHeader instead.
  const mac = desktop?.platform === 'mac';
  return <ConversationHead inset={inset} slim={mac} left={toggle && !mac ? <ShellToggle place="titlebar" /> : undefined} title={title} right={right} windowControls={desktop?.platform === 'windows' ? controls : undefined} />;
}
