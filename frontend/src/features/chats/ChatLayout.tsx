import { useEffect } from 'react';
import { Outlet, useLocation, useNavigate, useMatch } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Icons, RailItem, SplitHandle, cx } from '../../components/ui';
import { useShell } from '../../app/shell';
import { writeLocal } from '../../app/mode';
import { rememberWorkspacePath } from '../../app/workspacePath';
import ChatSidebar from './ChatSidebar';
import Workspace from '../workspace/Workspace';
import { WorkspaceProvider, useWorkspace } from '../workspace/WorkspaceProvider';
import { MAX_WIDTH, MIN_WIDTH, layoutColumns } from '../workspace/state';
import { useViewportWidth } from '../../app/useViewportWidth';

export default function ChatLayout() {
  const location = useLocation();
  const match = useMatch('/chat/:chatId');
  const chatId = match?.params.chatId === 'new' ? undefined : match?.params.chatId;
  useEffect(() => { writeLocal('cobot.mode', 'chat'); rememberWorkspacePath(location.pathname); if (chatId) writeLocal('cobot.chat.selected', chatId); }, [chatId, location.pathname]);
  return <WorkspaceProvider chatId={chatId}><ChatColumns chatId={chatId} /></WorkspaceProvider>;
}

function ChatColumns({ chatId }: { chatId?: string }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { listOpen } = useShell();
  const { state, setWidth } = useWorkspace();
  const width = useViewportWidth();
  const layout = layoutColumns(width, listOpen, state.open && !!chatId, state.width);
  return <div className="flex h-full bg-side text-neutral-900 dark:text-neutral-100">
    <nav aria-label={t('app.title')} className="flex w-14 shrink-0 flex-col items-center gap-1 bg-rail py-3">
      <RailItem icon={Icons.home} label={t('chat.new')} current={location.pathname === '/chat/new'} onClick={() => navigate('/chat/new')} />
      <div className="mt-auto"><RailItem icon={Icons.settings} label={t('nav.settings')} onClick={() => navigate('/settings')} /></div>
    </nav>
    {layout.showList && <aside aria-label={t('chat.history')} className="flex w-[min(16rem,40vw)] shrink-0 flex-col bg-side">
      <ChatSidebar />
    </aside>}
    <main className={cx('relative mt-2 min-w-0 flex-1 flex-col rounded-t-2xl bg-canvas shadow-sheet', layout.showConversation ? 'flex' : 'hidden')}><Outlet /></main>
    {state.open && chatId && layout.workspaceWidth > 0 && <>
      <SplitHandle value={state.width} min={MIN_WIDTH} max={MAX_WIDTH} label={t('workspace.resize')} onChange={setWidth} />
      <div style={layout.showConversation ? { width: layout.workspaceWidth } : undefined} className={cx('min-h-0', layout.showConversation ? 'shrink-0' : 'min-w-0 flex-1')}><Workspace /></div>
    </>}
  </div>;
}
