import { useEffect } from 'react';
import { Outlet, useLocation, useNavigate, useMatch } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { BrandMark, Icons, RailItem, SidebarHead, SplitHandle, cx } from '../../components/ui';
import { ConversationHeader, MenuTools, SidebarHeader, WindowCorner, useMacCorner, useMacWindowed } from '../../app/WindowChrome';
import { useShell, useShellListLayout } from '../../app/shell';
import { writeLocal } from '../../app/mode';
import { rememberWorkspacePath } from '../../app/workspacePath';
import ChatSidebar from './ChatSidebar';
import Workspace from '../workspace/Workspace';
import { WorkspaceProvider, useWorkspace } from '../workspace/WorkspaceProvider';
import { MAX_WIDTH, MIN_WIDTH, layoutColumns } from '../workspace/state';
import { useViewportWidth } from '../../app/useViewportWidth';

// The chat screen's columns reach the top of the window. The rail carries the
// logo above the new-chat action; on macOS the traffic-light corner stays
// clear in every window state (fullscreen reveals the lights on hover), so
// the logo always sits below it.
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
  const { state, setWidth, collapse } = useWorkspace();
  const width = useViewportWidth();
  const layout = layoutColumns(width, listOpen, state.open && !!chatId, state.width, state.fullView);
  useShellListLayout(layout.showList, collapse);
  const macWindowed = useMacWindowed();
  const macCorner = useMacCorner();
  return <div className="relative flex h-full bg-side text-neutral-900 dark:text-neutral-100">
    <WindowCorner />
    <nav aria-label={t('app.title')} className={cx('flex w-11 shrink-0 flex-col items-center gap-1 bg-rail py-3', macWindowed && 'pt-10')}>
      <div data-testid="rail-logo" className="flex h-10 w-full shrink-0 items-center justify-center"><BrandMark size={26} label={t('app.title')} /></div>
      <RailItem icon={Icons.home} label={t('chat.new')} current={location.pathname === '/chat/new'} onClick={() => navigate('/chat/new')} />
      <div className="mt-auto"><RailItem icon={Icons.settings} label={t('nav.settings')} onClick={() => navigate('/settings')} /></div>
    </nav>
    <SidebarHeader />
    {layout.showList && <aside aria-label={t('chat.history')} className="flex w-[min(16rem,40vw)] shrink-0 flex-col bg-side">
      {macCorner && <SidebarHead slim aria-hidden />}
      <MenuTools value="chat" />
      <ChatSidebar />
    </aside>}
    <main className={cx('min-w-0 flex-1 flex-col', layout.showConversation ? 'flex' : 'hidden')}>
      <ConversationHeader />
      <div className="relative mt-2 flex min-h-0 flex-1 flex-col rounded-t-2xl bg-canvas shadow-sheet"><Outlet /></div>
    </main>
    {state.open && chatId && layout.workspaceWidth > 0 && <>
      <SplitHandle value={state.width} min={MIN_WIDTH} max={MAX_WIDTH} label={t('workspace.resize')} onChange={setWidth} />
      <div style={layout.showConversation && !state.fullView ? { width: layout.workspaceWidth } : undefined} className={cx('min-h-0', layout.showConversation && !state.fullView ? 'shrink-0' : 'min-w-0 flex-1')}><Workspace /></div>
    </>}
  </div>;
}
