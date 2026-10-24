import { SidebarToggle } from '../app/SidebarToggle';
import { ModeSelector, modeHeaderClass, modeIcons } from '../app/ModeSelector';
import { useState } from 'react';
import '../i18n';
import { AppModeHeader, AppTitleBar, ChatComposer, Button, Card, EmptyState, Icons, Menu, IconButton } from '../components/ui';
import ArchiveTypeControl from '../features/settings/ArchiveTypeControl';
import Markdown from '../features/conversations/Markdown';
import ChatProcess from '../features/chats/ChatProcess';
import ChatWelcome from '../features/chats/ChatWelcome';
import { Chapter, Demo, Doc } from './parts';

export default function ChatPatterns() {
  const [listOpen, setListOpen] = useState(true);
  const [mode, setMode] = useState<'chat' | 'work'>('chat');
  const [draft, setDraft] = useState('继续讨论下一步');
  const [archiveType, setArchiveType] = useState<'chat' | 'team'>('chat');
  const [sorting, setSorting] = useState('recent');
  return <Chapter id="chat-patterns" title="无项目聊天" intro="聊天直接开始，团队支持多成员协作。聊天中的原生工具过程与审批仍然可见。">
    <Doc id="chat-mode" name="聊天 / 团队" file="app/ModeSelector.tsx" use="复用 40px 标题栏，四槽依次放图形 Logo、CoBot 字标、模式切换和二级菜单开关；胶囊为 24px 高，位于二级侧栏右侧；开关紧接会话区左边缘，内部使用图标；收起时字标和模式一起隐藏，只保留 Logo 和 36px 高的菜单开关，开关图标为 16×20px 的浅灰细线轮廓，悬停时加深；开关移位时背景立即清除，仅图标颜色保留过渡。" avoid="不能把聊天模式实现为禁用工具的权限开关。" a11y="沿用 SegmentedControl 的单选语义与方向键。">
      <Demo><AppTitleBar platform="windows" title="CoBot" flushLeft left={<div className={modeHeaderClass(listOpen)}><AppModeHeader brandLabel="CoBot" toggle={<SidebarToggle open={listOpen} onToggle={() => setListOpen(!listOpen)} openLabel="展开二级菜单" closeLabel="收起二级菜单" />}><ModeSelector label="使用模式" chatLabel="聊天" workLabel="团队" value={mode} onChange={setMode} /></AppModeHeader></div>} /></Demo>
    </Doc>
    <Doc id="mode-sidebar-actions" name="侧栏新建与列表标题" file="features/chats/ChatLayout.tsx · features/teams/TeamList.tsx" use="两种模式都先显示新建操作，再显示列表标题；操作图标与对应模式一致，使用现有 Button。">
      <Demo><div className="grid grid-cols-2 gap-4 bg-side">{(['chat', 'work'] as const).map((value) => <div key={value} className="px-3 pt-2 pb-2">
        <Button block variant="ghost" icon={modeIcons[value]} className="justify-start text-neutral-600 dark:text-neutral-300">{value === 'chat' ? '新建聊天' : '新建团队'}</Button>
        <p className="mt-4 px-3.5 text-xs font-medium text-neutral-500 dark:text-neutral-400">{value === 'chat' ? '聊天记录' : '团队列表'}</p>
      </div>)}</div></Demo>
    </Doc>
    <Doc id="chat-welcome" name="时间问候" file="features/chats/ChatWelcome.tsx" use="新聊天只显示一句本地时间问候；有草稿时保持稳定。" avoid="不放装饰图形、副标题或底部重复引导。" a11y="使用真实标题，时间变化不主动打断读屏。">
      <Demo><ChatWelcome hour={15} /><ChatComposer value={draft} onChange={setDraft} onSend={() => setDraft('')} sendLabel="发送" stopLabel="停止生成" placeholder="发送消息" options={<span className="px-2 text-xs">默认模型</span>} /></Demo>
    </Doc>
    <Doc id="chat-composer" name="聊天输入" file="components/ui/ChatComposer.tsx" use="聊天输入框和团队的输入框同样大小、同样位置（一行起、随文字长高）；运行中保留草稿，停止按钮始终可用。" avoid="不要禁用整个输入框导致草稿不能编辑。" a11y="Enter 发送、Shift+Enter 换行；中文输入法确认不发送。">
      <Demo><ChatComposer value={draft} onChange={setDraft} onSend={() => undefined} running canSend={false} onStop={() => undefined} sendLabel="发送" stopLabel="停止生成" placeholder="发送消息" options={<span className="px-2 text-xs">默认模型</span>} /></Demo>
    </Doc>
    <Doc id="chat-process" name="原生过程与结果" file="features/chats/ChatPage.tsx" use="原生步骤有可展开详情；文件链接按所属聊天打开。" avoid="不把子智能体变成团队成员。" a11y="过程状态有文字，详情使用原生 disclosure 语义。">
      <Demo><ChatProcess chatId="gallery" run={{ id: 'gallery-run', chatId: 'gallery', messageId: 'gallery-message', status: 'completed', threadId: '', turnId: '', epoch: '', errorKey: '', startedAt: '2026-10-24T00:00:00Z', endedAt: '2026-10-24T00:00:02Z', steps: [{ id: 'search', seq: 1, kind: 'web_search', status: 'completed', detail: { query: '城市历史' } }] }} />
        <Markdown text={'下面是一个示例：\n\n```js\nconsole.log("hello");\n```'} />
      </Demo>
    </Doc>
    <Doc id="chat-sidebar-menu" name="聊天整理与级联菜单" file="components/ui/Overlay.tsx" use="整理方式和排序通过子菜单选择；菜单挂到浮层，避免被滚动侧栏截断。" a11y="左右键进入和返回，Esc 逐级退出，焦点回到父项；窄窗口自动翻转。">
      <Demo><Menu label="整理聊天列表" trigger={<IconButton icon={Icons.more} label="整理聊天列表" />} items={[
        { id: 'sort', label: '聊天排序方式', icon: Icons.filter, children: [
          { id: 'recent', label: '最近更新', checked: sorting === 'recent', onSelect: () => setSorting('recent') },
          { id: 'manual', label: '手动排序', checked: sorting === 'manual', onSelect: () => setSorting('manual') },
        ] },
        { id: 'move', label: '移至分区', icon: Icons.openFolder, children: [{ id: 'learning', label: '学习', checked: false, onSelect: () => undefined }, { id: 'daily', label: '日常', checked: true, onSelect: () => undefined }] },
        'separator', { id: 'archive', label: '归档聊天', icon: Icons.archive, onSelect: () => undefined },
      ]} /></Demo>
    </Doc>
    <Doc id="unified-archive" name="已归档 · 聊天与团队" file="features/settings/ArchiveTypeControl.tsx" use="同一个归档设置入口按类型切换；各自保留删除与恢复语义。" avoid="切换类型不能保留上一类的批量选择。">
      <Demo><div className="space-y-4"><ArchiveTypeControl value={archiveType} onChange={setArchiveType} /><Card><EmptyState icon={<Icons.archive size={20} aria-hidden />} title={archiveType === 'chat' ? '没有已归档的聊天' : '没有已归档的团队'} /></Card></div></Demo>
    </Doc>
  </Chapter>;
}
