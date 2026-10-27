import { ReplyFileRow } from '../features/conversations/ResourceStrip';
import { TableContents } from '../features/workspace/TablePreview';
import { WorkbookContents } from '../features/workspace/WorkbookPreview';
import { LatexToolbar } from '../features/workspace/LatexToolbar';
import { DocumentToolbar } from '../features/workspace/DocumentToolbar';
import { PresentationToolbar } from '../features/workspace/PresentationToolbar';
import { PDFSearchBar } from '../features/workspace/PDFSearchBar';
import DiffView from '../components/DiffView';
import { ChangeFileRow } from '../features/workspace/ChangesTab';
import { PDFToolbar } from '../features/workspace/PDFPreview';
import { HTMLFrame } from '../features/workspace/HTMLPreview';
import { MediaPlayer } from '../features/workspace/MediaPreview';
import { NotebookContents } from '../features/workspace/NotebookPreview';
import { SidebarToggle } from '../app/SidebarToggle';
import { ModeSelector } from '../app/ModeSelector';
import { useState } from 'react';
import '../i18n';
import { ConversationHead, SidebarHead, BrandMark, RailItem, ChatComposer, List, Card, EmptyState, Icons, Menu, IconButton, TextInput } from '../components/ui';
import ArchiveTypeControl from '../features/settings/ArchiveTypeControl';
import Markdown from '../features/conversations/Markdown';
import ChatProcess from '../features/chats/ChatProcess';
import ChatWelcome from '../features/chats/ChatWelcome';
import { Chapter, Demo, Doc } from './parts';

export default function ChatPatterns() {
  const [changeFile, setChangeFile] = useState('src/app.ts');
  const [pdfPage, setPDFPage] = useState(1);
  const [slidePage, setSlidePage] = useState(0);
  const [pdfZoom, setPDFZoom] = useState(1);
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
    <Doc id="changes-preview" name="轮次变更文件" file="features/workspace/ChangesTab.tsx" use="左侧按轮次选文件，右侧读当轮 diff；当前文件另行打开。" avoid="旧记录统计未知时不展示伪精确数字；不提供撤销或 Git 操作。">
      <Demo><div className="max-w-sm"><List>{[
        { path: 'src/app.ts', kind: 'update' as const, additions: 12, deletions: 3, statsComplete: true, binary: false, diffTruncated: false },
        { path: 'old.md', movePath: 'docs/readme.md', kind: 'update' as const, additions: 0, deletions: 0, statsComplete: true, binary: false, diffTruncated: false },
        { path: 'legacy.txt', kind: 'delete' as const, additions: 0, deletions: 0, statsComplete: false, binary: false, diffTruncated: true },
      ].map((file) => <ChangeFileRow key={file.path} file={file} selected={changeFile === file.path} onSelect={() => setChangeFile(file.path)} />)}</List></div><DiffView diff={'--- a/app.ts\n+++ b/app.ts\n@@ -1 +1 @@\n-before\n+after'} maxLines={500} /></Demo>
    </Doc>
    <Doc id="reply-resources" name="回复文件资源" file="features/conversations/ResourceStrip.tsx" use="回复结束后显示经服务端确认的文件；打开时复用工作区标签页。">
      <Demo><List><ReplyFileRow item={{ target: 'docs/readme.md', file: { path: 'docs/readme.md', name: 'readme.md', kind: 'text', size: 120, version: 'demo', modifiedAt: '2026-10-26T00:00:00Z' } }} onOpen={() => setChangeFile('docs/readme.md')} /></List><p className="mt-2 text-xs text-neutral-500">已选择：{changeFile}</p></Demo>
    </Doc>
    <Doc id="latex-preview" name="LaTeX 编译与主文件" file="features/workspace/LatexToolbar.tsx" use="明确选择主文件、编译或取消，并在源码与上次成功的 PDF 之间切换。">
      <Demo><LatexToolbar main="main.tex" candidates={['main.tex', 'appendix.tex']} available pending={false} source hasPDF onMain={() => undefined} onCompile={() => undefined} onCancel={() => undefined} onSource={() => undefined} /></Demo>
      <Demo title="编译中"><LatexToolbar main="main.tex" candidates={[]} available pending source hasPDF={false} onMain={() => undefined} onCompile={() => undefined} onCancel={() => undefined} onSource={() => undefined} /></Demo>
    </Doc>
    <Doc id="pptx-preview" name="幻灯片导航" file="features/workspace/PresentationToolbar.tsx" use="从幻灯片列表选择或逐页查看，支持缩放与适应窗口。">
      <Demo><PresentationToolbar slides={['Overview', 'Details', 'Summary']} page={slidePage} zoom={pdfZoom} onPage={setSlidePage} onZoom={(value) => setPDFZoom(value ?? 1)} /></Demo>
    </Doc>
    <Doc id="docx-preview" name="文档缩放" file="features/workspace/DocumentToolbar.tsx" use="在隔离文档预览外控制缩放与显示渲染页面数。" avoid="页面数反映浏览器渲染结果，不承诺与 Word 完全相同。Office 超限时提示系统打开，不提示切换不存在的源码入口。">
      <Demo><DocumentToolbar pages={3} zoom={pdfZoom} onZoom={(value) => setPDFZoom(value ?? 1)} /></Demo>
    </Doc>
    <Doc id="workbook-preview" name="工作簿预览" file="features/workspace/WorkbookPreview.tsx" use="切换工作表查看已保存的单元格值，不执行宏或重算公式。">
      <Demo><div className="h-64"><WorkbookContents data={{ sheets: [{ name: 'Summary', data: { columns: 2, rows: [['Total', '123.00']] } }, { name: 'Details', data: { columns: 1, rows: [['Saved values']] } }] }} /></div></Demo>
    </Doc>
    <Doc id="table-preview" name="只读表格" file="features/workspace/TablePreview.tsx" use="以行列分页显示分隔文本，保留空值、换行和公式原文。">
      <Demo><div className="h-64"><TableContents data={{ columns: 3, rows: [['Name', 'Value', 'Notes'], ['demo', '=1+2', 'line 1\nline 2'], ['empty', '', '<script>text</script>']] }} /></div></Demo>
    </Doc>
    <Doc id="source-preview" name="源码只读预览" file="features/workspace/SourcePreview.tsx" use="有界分页、全文件搜索与 worker 高亮；高亮超时回退为纯文本。" avoid="不把源码插入为可执行 HTML。">
      <Demo><pre className="source-highlight overflow-auto p-3 font-mono text-xs"><code><span className="hljs-keyword">const</span>{' message = '}<span className="hljs-string">{'"hello"'}</span>{';\n'}<span className="hljs-comment">{'// 页面与搜索位置保持原始行列。'}</span></code></pre></Demo>
    </Doc>
    <Doc id="pdf-preview" name="PDF 导航与缩放" file="features/workspace/PDFPreview.tsx" use="逐页读取和展示，支持文字选择；导航与缩放按组换行。" avoid="不执行 PDF 的脚本或提交表单。">
      <Demo><PDFSearchBar query={draft} searching={false} count={3} current={0} limited={false} error={false} onQuery={setDraft} onSearch={() => setPDFPage(1)} onCancel={() => undefined} onSelect={(index) => setPDFPage(index + 1)} /><PDFToolbar page={pdfPage} pages={5} scale={pdfZoom} onPage={setPDFPage} onZoom={(value) => setPDFZoom(value ?? 1)} /></Demo>
    </Doc>
    <Doc id="html-preview" name="HTML 静态预览" file="features/workspace/HTMLPreview.tsx" use="在隔离框架中保留页面排版，资源由所属工作区读取。" avoid="不执行脚本，不允许页面导航；预览背景由文档决定。">
      <Demo><div className="flex h-52"><HTMLFrame title="HTML 预览示例" html={'<!doctype html><meta http-equiv="Content-Security-Policy" content="default-src \'none\'; style-src \'unsafe-inline\'"><style>body{font:14px system-ui;padding:16px}h1{font-size:20px}</style><h1>项目说明</h1><p>这是只读静态页面。</p>'} /></div></Demo>
    </Doc>
    <Doc id="media-preview" name="音视频只读预览" file="features/workspace/MediaPreview.tsx" use="浏览器原生播放、进度和音量控件。" avoid="不自动播放；损坏或平台不支持的编码显示明确错误。">
      <Demo><div className="h-24"><MediaPlayer kind="audio" name="音频示例" url="data:audio/wav;base64,UklGRsQAAABXQVZFZm10IBAAAAABAAEAQB8AAIA+AAACABAAZGF0YaAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" /></div></Demo>
      <Demo title="小尺寸视频适应区域，保留可用的原生播放控件"><div className="h-64 w-full max-w-lg"><MediaPlayer kind="video" name="视频示例" url="data:video/mp4;base64,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" /></div></Demo>
    </Doc>
    <Doc id="notebook-preview" name="Notebook 只读预览" file="features/workspace/NotebookPreview.tsx" use="代码单元、Markdown 与已有输出；分页限制同时渲染的单元。" avoid="不执行代码；HTML 输出隔离，外部资源默认阻止。">
      <Demo><div className="h-96"><NotebookContents scope={{ kind: 'chat', id: 'gallery' }} path="sample.ipynb" cells={[
        { kind: 'markdown', source: '# 数据分析\n以下是保存的运行结果。', execution: null, outputs: [], truncated: false },
        { kind: 'code', source: 'sum([1, 2, 3])', execution: 1, outputs: [{ kind: 'text', value: '6' }], truncated: false },
      ]} /></div></Demo>
    </Doc>
    <Doc id="unified-archive" name="已归档 · 聊天与团队" file="features/settings/ArchiveTypeControl.tsx" use="同一个归档设置入口按类型切换；各自保留删除与恢复语义。" avoid="切换类型不能保留上一类的批量选择。">
      <Demo><div className="space-y-4"><ArchiveTypeControl value={archiveType} onChange={setArchiveType} /><Card><EmptyState icon={<Icons.archive size={20} aria-hidden />} title={archiveType === 'chat' ? '没有已归档的聊天' : '没有已归档的团队'} /></Card></div></Demo>
    </Doc>
  </Chapter>;
}
