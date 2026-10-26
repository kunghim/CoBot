import { SidebarToggle } from '../app/SidebarToggle';
import { ModeSelector } from '../app/ModeSelector';
import { useState } from 'react';
import '../i18n';
import { ConversationHead, SidebarHead, BrandMark, RailItem, ChatComposer, Card, EmptyState, Icons, Menu, IconButton, TextInput } from '../components/ui';
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
    <Doc id="chat-mode" name="聊天 / 团队" file="app/ModeSelector.tsx" use="栏目通到窗口顶部，各自画 40px 列头：Logo 位于主导航的主页按钮上方，不显示 CoBot 字标；窗口顶行（SidebarHead）在 macOS 上是系统按钮的空拖拽带；搜索框与模式胶囊在其下的二级菜单工具行内——白底描边胶囊（controlFramePill），24px 高，窄窗口先收缩搜索框；会话列头（ConversationHead）左端是 36px 高的菜单开关，图标为 16×20px 的浅灰细线轮廓，悬停时加深，移位时背景立即清除；收起侧栏时侧栏列头随之隐藏，开关仍跟随会话区左边缘；macOS 窗口态左上角是 88×40 单色系统按钮预留，全屏取消，搜索框与胶囊不与红绿灯重叠。" avoid="不能把聊天模式实现为禁用工具的权限开关；不另加整窗大顶栏。" a11y="沿用 SegmentedControl 的单选语义与方向键。">
      <Demo><div className="overflow-hidden rounded-xl bg-side"><div className="relative flex"><div className="h-10 w-11 shrink-0" aria-hidden />{listOpen && <div className="w-[min(16rem,40vw)] shrink-0"><SidebarHead /><div className="flex h-10 shrink-0 items-center gap-2 bg-side px-2"><div className="flex min-w-0 flex-1 items-center justify-between gap-2"><TextInput size="xs" pill placeholder="搜索会话" prefix={<Icons.search size={13} aria-hidden />} /><ModeSelector label="使用模式" chatLabel="聊天" workLabel="团队" value={mode} onChange={setMode} /></div></div></div>}<ConversationHead left={<SidebarToggle open={listOpen} onToggle={() => setListOpen(!listOpen)} openLabel="展开二级菜单" closeLabel="收起二级菜单" />} /></div><div className="flex h-40"><div className="flex w-11 shrink-0 flex-col items-center gap-1 bg-rail py-3"><div className="flex h-10 items-center"><BrandMark size={26} label="CoBot" /></div><RailItem icon={Icons.home} label="主页" /></div>{listOpen && <div className="w-[min(16rem,40vw)] shrink-0 p-3 text-xs text-neutral-500">二级菜单</div>}<div className="min-w-0 flex-1 p-3 text-xs text-neutral-500">会话</div></div></div></Demo>
    </Doc>
    <Doc id="mode-sidebar-actions" name="侧栏列表标题与新建" file="features/chats/ChatSidebar.tsx · features/teams/TeamList.tsx" use="两种模式的列表标题行左侧是标题、右侧是紧凑加号（新建聊天/新建团队）加整理菜单；一级导航常驻新建入口，列表顶部不再放大号新建按钮。">
      <Demo><div className="grid grid-cols-2 gap-4 bg-side">{(['chat', 'work'] as const).map((value) => <div key={value} className="flex items-center justify-between px-4 pb-1 pt-2">
        <p className="text-xs font-medium text-neutral-500 dark:text-neutral-400">{value === 'chat' ? '聊天记录' : '团队列表'}</p>
        <div className="flex items-center gap-0.5">
          <IconButton size="sm" icon={Icons.add} label={value === 'chat' ? '新建聊天' : '新建团队'} />
          <IconButton size="sm" icon={Icons.more} label="整理" />
        </div>
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
