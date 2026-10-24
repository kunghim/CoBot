import { useEffect } from 'react';
import { Outlet, useLocation, useNavigate, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Icons, RailItem } from '../../components/ui';
import { useShell } from '../../app/shell';
import { writeLocal } from '../../app/mode';
import { rememberWorkspacePath } from '../../app/workspacePath';
import ChatSidebar from './ChatSidebar';

export default function ChatLayout() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { chatId } = useParams();
  const { listOpen } = useShell();
  useEffect(() => { writeLocal('cobot.mode', 'chat'); rememberWorkspacePath(location.pathname); if (chatId) writeLocal('cobot.chat.selected', chatId); }, [chatId, location.pathname]);
  return <div className="flex h-full bg-side text-neutral-900 dark:text-neutral-100">
    <nav aria-label={t('app.title')} className="flex w-14 shrink-0 flex-col items-center gap-1 bg-rail py-3">
      <RailItem icon={Icons.home} label={t('chat.new')} current={location.pathname === '/chat/new'} onClick={() => navigate('/chat/new')} />
      <div className="mt-auto"><RailItem icon={Icons.settings} label={t('nav.settings')} onClick={() => navigate('/settings')} /></div>
    </nav>
    {listOpen && <aside aria-label={t('chat.history')} className="flex w-[min(16rem,40vw)] shrink-0 flex-col bg-side">
      <ChatSidebar />
    </aside>}
    <main className="relative mt-2 flex min-w-0 flex-1 flex-col rounded-t-2xl bg-canvas shadow-sheet"><Outlet /></main>
  </div>;
}
