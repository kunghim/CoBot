// Gallery: the rules everything is built from — principles, color, type,
// space, shape, depth, motion, focus and icons.
import { Chapter, Demo, Doc, Rules } from './parts';
import { Icons, cx, iconMeaning, toneSoft, toneSolid, type IconName } from '../components/ui';

// Written out in full so the class names exist in the built CSS.
const NEUTRALS: Array<[string, string]> = [
  ['50', 'bg-neutral-50'], ['100', 'bg-neutral-100'], ['200', 'bg-neutral-200'], ['300', 'bg-neutral-300'], ['400', 'bg-neutral-400'], ['500', 'bg-neutral-500'],
  ['600', 'bg-neutral-600'], ['700', 'bg-neutral-700'], ['800', 'bg-neutral-800'], ['900', 'bg-neutral-900'], ['950', 'bg-neutral-950'],
];
const NEUTRAL_USE: Record<string, string> = {
  '50': '页面底色（浅）', '100': '下沉区域、侧栏（浅）', '200': '分隔线、描边（浅）', '300': '输入框边框', '400': '占位文字、次要图标',
  '500': '辅助文字', '600': '次要正文', '700': '强调正文（深色模式描边）', '800': '深色模式卡片描边', '900': '主文字、主要按钮、深色卡片', '950': '页面底色（深）',
};
const ROLE_COLORS: Array<[string, string, string]> = [
  ['队长', 'leader', '#7d8c85'], ['产品经理', 'product-manager', '#d8b1fb'], ['前端开发', 'frontend-dev', '#9cc8fb'],
  ['后端开发', 'backend-dev', '#6fd6c8'], ['测试工程师', 'qa-engineer', '#fbb271'], ['评审员', 'code-reviewer', '#f2d16b'],
];

export default function Foundations() {
  return (
    <Chapter id="foundations" title="基础规范" intro="颜色、字体、间距、圆角、阴影、层级、动效和焦点。组件只从这里取值；用了规范之外的值，要在代码注释里写清原因。">
      <Doc id="principles" name="设计原则" file="frontend/src/styles/index.css" use="判断一个界面“对不对”时的依据。">
        <Rules
          items={[
            '安静的桌面，颜色留给成员：界面主体是带一点绿意的石墨灰，强调色只出现在焦点、选中和主要操作上；最鲜明的颜色是成员的角色色。',
            '一个视图只有一个主要操作（实心深色按钮）；其他操作用次要或文字按钮。',
            '状态要看得见：每个可点的东西都有静止、悬停、按下、选中、键盘焦点、禁用几种可分辨的样子。',
            '悬停和按下用“半透明墨色叠层”，不用固定灰色——固定灰色放在同色底上就看不见了。',
            '机器的值（路径、ID、数字、命令）用等宽字体；人说的话用正文字体。',
            '同一件事在所有地方长得一样、叫法一样、在同一个位置。',
            '主题只换中性色和强调色；成员角色色、状态色在所有主题和明暗模式下不变。',
          ]}
        />
      </Doc>

      <Doc id="color" name="颜色" file="frontend/src/styles/index.css · components/ui/foundation.ts" use="所有颜色都从这几组里取。切换顶部的主题和明暗，看它们怎么变。" avoid="不要写死十六进制颜色（成员头像色板除外），不要用 Tailwind 的 gray、slate 等其他灰色。">
        <Demo title="中性色阶（随主题变化）">
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-6">
            {NEUTRALS.map(([step, bg]) => (
              <div key={step} className="overflow-hidden rounded-lg border border-neutral-200 dark:border-neutral-800">
                <div className={cx('h-12', bg)} />
                <div className="px-2 py-1.5 text-[11px] leading-4">
                  <b className="font-mono">neutral-{step}</b>
                  <span className="block text-neutral-500 dark:text-neutral-400">{NEUTRAL_USE[step]}</span>
                </div>
              </div>
            ))}
          </div>
        </Demo>
        <Demo title="强调色 signal（随主题变化）：焦点、选中、链接、开关">
          <div className="flex flex-wrap gap-3">
            {[['signal-400', 'bg-signal-400', '深色模式文字'], ['signal-500', 'bg-signal-500', '焦点描边、选中'], ['signal-600', 'bg-signal-600', '链接、开关']].map(([name, bg, use]) => (
              <div key={name} className="flex items-center gap-2 text-xs">
                <span className={cx('h-8 w-8 rounded-lg', bg)} />
                <span>
                  <b className="font-mono">{name}</b>
                  <span className="block text-neutral-500 dark:text-neutral-400">{use}</span>
                </span>
              </div>
            ))}
          </div>
        </Demo>
        <Demo title="状态色（不随主题变化）">
          <div className="flex flex-wrap gap-3">
            {(['neutral', 'info', 'success', 'warning', 'danger'] as const).map((tone) => (
              <div key={tone} className="flex items-center gap-2">
                <span className={cx('h-3 w-3 rounded-full', toneSolid[tone])} />
                <span className={cx('rounded-full px-2 py-0.5 text-xs font-medium', toneSoft[tone])}>
                  {{ neutral: '中性', info: '信息 · 排队', success: '成功 · 工作中', warning: '警告 · 等你处理', danger: '错误 · 中断' }[tone]}
                </span>
              </div>
            ))}
          </div>
        </Demo>
        <Demo title="角色色（不随主题变化，来自头像库色板）">
          <div className="flex flex-wrap gap-3">
            {ROLE_COLORS.map(([name, id, hex]) => (
              <span key={id} className="flex items-center gap-2 text-xs">
                <span className="h-6 w-6 rounded-md" style={{ background: hex }} />
                <span>
                  {name}
                  <code className="block font-mono text-[10px] text-neutral-500">{hex}</code>
                </span>
              </span>
            ))}
          </div>
        </Demo>
      </Doc>

      <Doc id="type" name="字体与字号" file="--font-sans · --font-mono" use="正文字体：Segoe UI Variable / PingFang SC / 微软雅黑；等宽字体：Cascadia Code / JetBrains Mono。" avoid="不要引入网络字体；不要在一个视图里用超过四种字号。">
        <Demo>
          <div className="space-y-3">
            {[
              ['text-2xl font-semibold tracking-tight', '24 · 页面大标题（画廊、空状态主标题）'],
              ['text-xl font-semibold tracking-tight', '20 · 页面标题'],
              ['text-lg font-semibold tracking-tight', '18 · 对话框标题'],
              ['text-base font-semibold', '16 · 分区标题'],
              ['text-sm', '14 · 正文、控件文字（默认）'],
              ['text-xs', '12 · 辅助说明、提示、标签'],
              ['text-[11px] font-semibold uppercase tracking-[0.12em]', '11 · 分组小标题（全大写）'],
              ['font-mono text-xs', '12 · 等宽：E:\\work\\todo-app · 1,280 tok'],
            ].map(([cls, label]) => (
              <p key={label} className={cls}>{label}</p>
            ))}
          </div>
        </Demo>
      </Doc>

      <Doc id="space" name="间距、圆角与阴影" file="Tailwind 默认 4px 刻度" use="间距用 4 的倍数：控件内 8–12，控件之间 8，分组之间 16–24，分区之间 32。">
        <Demo title="圆角：越大的东西越圆">
          <div className="flex flex-wrap items-end gap-4 text-xs">
            {[['rounded', '4 · 小标记、Kbd'], ['rounded-md', '6 · 徽章、菜单项'], ['rounded-lg', '8 · 按钮、输入框'], ['rounded-xl', '12 · 卡片、浮层'], ['rounded-2xl', '16 · 对话框、输入区'], ['rounded-full', '圆 · 头像状态点、开关']].map(([cls, label]) => (
              <div key={cls} className="flex flex-col items-center gap-2">
                <span className={cx('h-14 w-14 border-2 border-neutral-400 bg-neutral-900/[0.04] dark:border-neutral-500', cls)} />
                {label}
              </div>
            ))}
          </div>
        </Demo>
        <Demo title="深度：阴影只给浮起来的东西">
          <div className="flex flex-wrap gap-6 text-xs">
            {[['', '平面：页面里的卡片用描边，不用阴影'], ['shadow-sm', '轻：按钮、输入框、选中的列表行'], ['shadow-xl ring-1 ring-neutral-900/10', '浮层：菜单、提示、Toast'], ['shadow-2xl', '最高：对话框、侧滑面板']].map(([cls, label]) => (
              <div key={label} className={cx('w-40 rounded-xl border border-neutral-200 bg-white p-3 dark:border-neutral-800 dark:bg-neutral-900', cls)}>{label}</div>
            ))}
          </div>
        </Demo>
        <Demo title="层级（z-index）：从低到高">
          <p className="font-mono text-xs leading-6">sticky 10 · dropdown/sheet 40 · dialog 50 · toast 60 · tooltip 70（components/ui/foundation.ts 的 layer）</p>
        </Demo>
      </Doc>

      <Doc id="motion" name="动效" file="--animate-*" use="出现的东西轻轻上浮淡入（160ms）；侧滑面板从边缘滑入（200ms）；悬停变化 150ms。" avoid="不要弹跳、不要装饰性动画。系统开了“减少动态效果”时，所有动画都会被缩到几乎为零。">
        <Demo>
          <div className="flex flex-wrap gap-4 text-xs">
            {[['animate-pop-in', 'pop-in：菜单、Toast、对话框'], ['animate-fade-in', 'fade-in：遮罩、提示'], ['animate-shimmer', 'shimmer：骨架屏'], ['animate-spin', 'spin：加载中']].map(([cls, label]) => (
              <div key={cls} className="flex items-center gap-2">
                <span className={cx('inline-block h-6 w-6 rounded-md bg-signal-500', cls)} style={{ animationIterationCount: 'infinite', animationDuration: cls === 'animate-spin' ? undefined : '1.6s' }} />
                {label}
              </div>
            ))}
          </div>
        </Demo>
      </Doc>

      <Doc id="focus" name="键盘焦点" file="index.css 的 :focus-visible" use="所有可聚焦的元素用键盘聚焦时，统一显示 2px 强调色描边。用 Tab 键在本页试试。" avoid="不要写 outline-none 却不给替代的焦点样式；不要在鼠标点击时显示焦点框（用 focus-visible，不用 focus）。">
        <Demo>
          <div className="flex flex-wrap gap-3">
            <button type="button" className="rounded-lg border border-neutral-300 px-3 py-1.5 text-sm dark:border-neutral-700">按 Tab 聚焦我</button>
            <a href="#focus" className="rounded text-sm text-signal-600 underline-offset-4 hover:underline dark:text-signal-400">或者这个链接</a>
          </div>
        </Demo>
      </Doc>

      <Doc
        id="icons"
        name="图标"
        file="components/ui/icons.ts（Icons）"
        use="只用 lucide 图标，并且按意思从 Icons 里取（Icons.team、Icons.delete），不在页面里随手挑。一个意思只有一个图标。尺寸：14（行内、小控件）、16（按钮、导航项）、18（图标栏、对话框）。图标和旁边文字同色。"
        avoid="不要同一个功能用两个图标；只有图标的按钮必须有文字标签（IconButton 的 label）。需要新的意思，先加进 Icons 和这张表。"
      >
        <Demo>
          <div className="grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
            {(Object.keys(Icons) as IconName[]).map((name) => {
              const Icon = Icons[name];
              return (
                <div key={name} className="flex items-center gap-2.5 rounded-lg px-2 py-1.5 text-xs hover:bg-neutral-900/[0.04] dark:hover:bg-white/[0.05]">
                  <Icon size={16} aria-hidden className="shrink-0 text-neutral-700 dark:text-neutral-200" />
                  <span className="min-w-0">
                    <span className="block truncate text-neutral-800 dark:text-neutral-100">{iconMeaning[name]}</span>
                    <code className="block truncate font-mono text-[10px] text-neutral-500">Icons.{name}</code>
                  </span>
                </div>
              );
            })}
          </div>
        </Demo>
        <Demo title="尺寸">
          <div className="flex items-end gap-6 text-xs text-neutral-700 dark:text-neutral-200">
            {[14, 16, 18].map((size) => (
              <div key={size} className="flex flex-col items-center gap-2">
                <span className="flex gap-2">
                  <Icons.home size={size} aria-hidden />
                  <Icons.settings size={size} aria-hidden />
                  <Icons.notification size={size} aria-hidden />
                </span>
                {size}px
              </div>
            ))}
          </div>
        </Demo>
      </Doc>    </Chapter>
  );
}
