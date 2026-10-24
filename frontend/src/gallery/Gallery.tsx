// The component gallery: CoBot's design system on one page. It is rendered
// to a single static HTML file (npm run gallery) that anyone changing the
// interface — person or AI — reads first. It shows the real components.
import { useEffect, useState } from 'react';
import { cx } from '../components/ui';
import { Monitor, Moon, Sun } from 'lucide-react';
import { applyAppearance, type ColorMode, type Theme } from '../app/appearance';
import { SegmentedControl, ToastProvider } from '../components/ui';
import Actions from './Actions';
import FeedbackData from './FeedbackData';
import Foundations from './Foundations';
import NavigationOverlays from './NavigationOverlays';
import Patterns from './Patterns';
import TeamConversation from './TeamConversation';
import ChatPatterns from './ChatPatterns';
import { Rules } from './parts';

const TOC: Array<[string, string, Array<[string, string]>]> = [
  ['must-read', '必读', []],
  ['foundations', '基础规范', [['brand-mark', '品牌标志'], ['principles', '原则'], ['color', '颜色'], ['type', '字体'], ['space', '间距与形状'], ['motion', '动效'], ['focus', '焦点'], ['icons', '图标']]],
  ['actions', '操作与表单', [['button', 'Button'], ['icon-button', 'IconButton'], ['button-group', 'ButtonGroup'], ['field', 'Field'], ['text-input', '输入框'], ['select', 'Select · Combobox'], ['checkbox', 'Checkbox · Switch'], ['radio', 'Radio · RadioCards'], ['checkbox-cards', 'CheckboxCards'], ['segmented', 'Segmented · Toggle'], ['chip', 'Chip 小片'], ['slider', 'Slider'], ['file-drop', 'FileDrop'], ['fieldset', 'Fieldset']]],
  ['navigation', '导航与浮层', [['rail', 'RailItem'], ['nav-item', 'NavItem'], ['tabs', 'Tabs'], ['tree', 'Tree'], ['breadcrumbs', '面包屑 · 步骤 · 分页'], ['menu', 'Menu'], ['popover', 'Popover · Tooltip'], ['dialog', 'Dialog · AlertDialog'], ['sheet', 'Sheet'], ['workspace', '工作区标签条']]],
  ['feedback', '反馈、数据与结构', [['toast', 'Toast'], ['alert', 'Alert'], ['progress', '加载'], ['progress-ring', 'ProgressRing'], ['empty', 'EmptyState'], ['badge', 'Badge 等'], ['logo-tile', 'LogoTile 字母块'], ['card', 'Card'], ['list', 'List'], ['table', 'Table'], ['description-list', '键值 · 指标 · 代码'], ['accordion', 'Accordion · 复制'], ['structure', '页面结构']]],
  ['team', '成员与会话', [['avatar', 'Avatar'], ['role-chip', 'RoleChip'], ['member-row', 'MemberRow'], ['messages', '消息'], ['process', 'ProcessStep'], ['approval', '审批卡 · 提问卡'], ['composer', 'Composer'], ['model-picker', '模型与思考强度'], ['title-bar', '桌面标题栏']]],
  ['patterns', '模式、文案、禁止', [['saving', '保存反馈'], ['panels', '侧栏开合'], ['states', '加载 · 空 · 出错'], ['writing', '文案'], ['donts', '禁止事项']]],
];

export default function Gallery({ generatedAt }: { generatedAt: string }) {
  const [theme, setTheme] = useState<Theme>('classic');
  const [mode, setMode] = useState<ColorMode>('light');
  useEffect(() => applyAppearance({ theme, colorMode: mode }), [theme, mode]);
  // The table of contents follows the entry being read.
  const [current, setCurrent] = useState('must-read');
  useEffect(() => {
    const ids = TOC.flatMap(([id, , children]) => [id, ...children.map(([childId]) => childId)]);
    const update = () => {
      let found = ids[0];
      for (const id of ids) {
        const element = document.getElementById(id);
        if (element && element.getBoundingClientRect().top < 120) found = id;
      }
      setCurrent(found);
    };
    update();
    window.addEventListener('scroll', update, { passive: true });
    return () => window.removeEventListener('scroll', update);
  }, []);

  return (
    <ToastProvider closeLabel="关闭">
      <div className="min-h-full bg-neutral-50 text-neutral-900 dark:bg-neutral-950 dark:text-neutral-100">
        <header className="sticky top-0 z-30 border-b border-neutral-200 bg-neutral-50/90 backdrop-blur dark:border-neutral-800 dark:bg-neutral-950/90">
          <div className="mx-auto flex max-w-[1400px] flex-wrap items-center gap-4 px-6 py-3">
            <div className="mr-auto">
              <p className="text-base font-semibold tracking-tight">CoBot 组件画廊</p>
              <p className="text-xs text-neutral-500 dark:text-neutral-400">生成于 {generatedAt} · 由 frontend/src/gallery 生成，勿手改</p>
            </div>
            <SegmentedControl
              label="主题"
              size="sm"
              value={theme}
              onChange={setTheme}
              options={[{ value: 'classic', label: '经典' }, { value: 'graphite', label: '石墨' }, { value: 'harbor', label: '港湾' }, { value: 'dusk', label: '暮梅' }]}
            />
            <SegmentedControl
              label="明暗"
              size="sm"
              value={mode}
              onChange={setMode}
              options={[{ value: 'light', label: '浅色', icon: Sun }, { value: 'dark', label: '深色', icon: Moon }, { value: 'system', label: '系统', icon: Monitor }]}
            />
          </div>
        </header>

        <div className="mx-auto flex max-w-[1400px] gap-10 px-6">
          <nav aria-label="目录" className="sticky top-20 hidden h-[calc(100vh-6rem)] w-52 shrink-0 overflow-y-auto py-8 text-sm lg:block">
            {TOC.map(([id, title, children]) => (
              <div key={id} className="mb-4">
                <a href={`#${id}`} aria-current={current === id ? 'location' : undefined} className={cx('block rounded-md px-2 py-1 font-semibold hover:bg-neutral-900/[0.06] dark:hover:bg-white/[0.07]', current === id && 'text-signal-600 dark:text-signal-400')}>{title}</a>
                {children.map(([childId, label]) => (
                  <a key={childId} href={`#${childId}`} aria-current={current === childId ? 'location' : undefined} className={cx('block rounded-md border-l-2 px-2 py-0.5 text-sm hover:bg-neutral-900/[0.06] hover:text-neutral-900 dark:hover:bg-white/[0.07] dark:hover:text-neutral-100', current === childId ? 'border-signal-500 font-medium text-neutral-900 dark:text-neutral-50' : 'border-transparent text-neutral-500 dark:text-neutral-400')}>{label}</a>
                ))}
              </div>
            ))}
          </nav>

          <main className="min-w-0 flex-1 space-y-20 py-10">
            <section id="must-read" className="scroll-mt-20 rounded-2xl border border-neutral-200 bg-white p-6 dark:border-neutral-800 dark:bg-neutral-900">
              <h1 className="text-2xl font-semibold tracking-tight">必读：改前端之前</h1>
              <p className="mt-2 max-w-3xl text-sm leading-6 text-neutral-600 dark:text-neutral-300">
                这一页是 CoBot 界面的唯一标准。人或 AI 修改 frontend/ 之前先读本节，再到下面找要用的组件。
              </p>
              <div className="mt-5">
                <Rules
                  items={[
                    '只用 frontend/src/components/ui 里的组件拼界面（从 "../components/ui" 引入）。需要的东西这里没有，先把组件和它的画廊条目加上，再在界面里用。',
                    '颜色只用中性色阶 neutral-*、强调色 signal-*、状态色（emerald/sky/amber/red）和角色色；不写死十六进制，不用 gray/slate/zinc。画在强调色上的字和图标用 on-signal，不写 white（经典主题深色模式下强调色是近白）。',
                    '悬停、按下、选中用 interactive.hover / pressed / selected（半透明墨色叠层），不要用某个固定灰色。',
                    '每个可点的元素都要有可分辨的静止、悬停、选中、键盘焦点、禁用状态。焦点框由全局样式提供，不要写 outline-none 又不给替代。',
                    '一个视图只有一个 primary 按钮；不可撤销的操作用 danger 按钮并先弹 AlertDialog。',
                    '保存成功用 Toast（窗口顶部居中）；和字段有关的错误写在字段下面；不在字段旁写“已保存”。Alert 是页面内容的一部分，放在相关内容上方。',
                    '按钮形状：页面、设置、表单用 rounded；会话里的卡片和浮动卡片（审批、提问）用 pill，答复按钮靠右，主要的放最右。',
                    '图标从 Icons 按意思取（Icons.team、Icons.delete），一个意思一个图标。',
                    '会话里提到成员时用“头像 + 名字”；RoleChip 只用在放不下头像的紧凑场合。',
                    '侧栏开合用 PanelToggle：同一个按钮、同一个位置、同一对图标；二级菜单开关位于紧凑 AppTitleBar 的模式切换之后，网页与桌面保持一致。',
                    '只有图标的按钮必须有 label；所有文字走 i18n，两个语言包键名一致。',
                    '机器的值（路径、ID、数字、命令）用等宽字体（mono）。',
                    '改完界面后：重新生成本画廊（npm run gallery），并在四个主题、深浅两种模式下看一遍。',
                  ]}
                />
              </div>
            </section>
            <Foundations />
            <Actions />
            <NavigationOverlays />
            <FeedbackData />
            <TeamConversation />
            <ChatPatterns />
            <Patterns />
          </main>
        </div>
      </div>
    </ToastProvider>
  );
}
