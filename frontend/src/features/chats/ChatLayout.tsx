import { useEffect } from 'react';
import { Outlet, useLocation, useNavigate, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { BrandMark, Icons, RailItem, cx } from '../../components/ui';
import { ConversationHeader, SidebarHeader, WindowCorner, useMacCorner } from '../../app/WindowChrome';
import { useShell, useShellListLayout } from '../../app/shell';
import { writeLocal } from '../../app/mode';
import { rememberWorkspacePath } from '../../app/workspacePath';
import ChatSidebar from './ChatSidebar';

// The chat screen's columns reach the top of the window. The rail carries the
// logo above the new-chat action; on macOS the traffic-light corner stays
// clear in every window state (fullscreen reveals the lights on hover), so
// the logo always sits below it.
export default function ChatLayout() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { chatId } = useParams();
  const { listOpen } = useShell();
  useShellListLayout(listOpen);
  useEffect(() => { writeLocal('cobot.mode', 'chat'); rememberWorkspacePath(location.pathname); if (chatId) writeLocal('cobot.chat.selected', chatId); }, [chatId, location.pathname]);
  return <div className="relative flex h-full bg-side text-neutral-900 dark:text-neutral-100">
    <WindowCorner />
    <nav aria-label={t('app.title')} className={cx('flex w-14 shrink-0 flex-col items-center gap-1 bg-rail py-3', useMacCorner() && 'pt-12')}>
      <div data-testid="rail-logo" className="flex h-10 w-full shrink-0 items-center justify-center"><BrandMark size={26} label={t('app.title')} /></div>
      <RailItem icon={Icons.home} label={t('chat.new')} current={location.pathname === '/chat/new'} onClick={() => navigate('/chat/new')} />
      <div className="mt-auto"><RailItem icon={Icons.settings} label={t('nav.settings')} onClick={() => navigate('/settings')} /></div>
    </nav>
    {listOpen && <aside aria-label={t('chat.history')} className="flex w-[min(16rem,40vw)] shrink-0 flex-col bg-side">
      <SidebarHeader />
      <ChatSidebar />
    </aside>}
    <main className="flex min-w-0 flex-1 flex-col">
      <ConversationHeader />
      <div className="relative mt-2 flex min-h-0 flex-1 flex-col rounded-t-2xl bg-canvas shadow-sheet"><Outlet /></div>
    </main>
  </div>;
}
