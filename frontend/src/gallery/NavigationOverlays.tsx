// Gallery: navigation and things that float.
import { useState } from 'react';
import { Archive, Copy, Home, MoreHorizontal, Palette, Pencil, Settings, Settings2, ShieldCheck, Smile, Trash2, Users } from 'lucide-react';
import { AlertDialog, Breadcrumbs, Button, ComposerOption, Count, Dialog, Field, FloatingCard, IconButton, Menu, NavItem, NavSection, Pagination, Popover, RailItem, Sheet, SplitHandle, Steps, TabPanel, TabStrip, Tabs, TextInput, Tooltip, Tree, shownState } from '../components/ui';
import { Chapter, Demo, Doc, Matrix } from './parts';

const MODES = [
  { label: '只读', description: '只能查看和分析，修改前都会问你' },
  { label: '默认权限', description: '可在团队目录内自由读写和执行，超出范围时会问你' },
  { label: '完全访问', description: '不再询问，也不受目录限制' },
];

export default function NavigationOverlays() {
  const [rail, setRail] = useState('home');
  const [nav, setNav] = useState('appearance');
  const [tab, setTab] = useState<'tasks' | 'approvals' | 'activity' | 'memory'>('tasks');
  const [pill, setPill] = useState<'all' | 'open' | 'done'>('open');
  const [page, setPage] = useState(3);
  const [node, setNode] = useState('lead');
  const [dialog, setDialog] = useState(false);
  const [alert, setAlert] = useState(false);
  const [sheet, setSheet] = useState<'left' | 'right' | null>(null);
  const [last, setLast] = useState('');
  const [mode, setMode] = useState('默认权限');
  const [wsTabs, setWsTabs] = useState([
    { id: 'browser:t1', label: '测试工程师 · 我的待办' },
    { id: 'file:index.html', label: 'index.html' },
    { id: 'changes', label: '变更' },
  ]);
  const [wsActive, setWsActive] = useState('browser:t1');
  const [wsWidth, setWsWidth] = useState(560);
  const [card, setCard] = useState(false);

  return (
    <Chapter id="navigation" title="导航与浮层" intro="导航把人带到别处；浮层临时盖在页面上。所有导航项都有静止、悬停、当前三种看得出的状态；所有浮层都能用 Esc 关闭，关闭后焦点回到打开它的按钮。">
      <Doc id="rail" name="RailItem 一级图标栏" file="components/ui/Navigation.tsx" use="最左侧的一级导航（首页、设置）。当前页是填充的方块。" a11y="每项都有 label，当前项标记 aria-current=page。">
        <Demo surface="sunken">
          <div className="flex w-11 flex-col items-center gap-1 rounded-xl py-2">
            <RailItem icon={Home} label="首页" current={rail === 'home'} onClick={() => setRail('home')} />
            <RailItem icon={Users} label="团队" current={rail === 'teams'} onClick={() => setRail('teams')} badge={<Count value={2} tone="danger" label="2 项等你处理" />} />
            <RailItem icon={Settings} label="设置" current={rail === 'settings'} onClick={() => setRail('settings')} />
          </div>
        </Demo>
      </Doc>

      <Doc id="nav-item" name="NavItem 侧边导航项 · NavSection 分组" file="components/ui/Navigation.tsx" use="侧栏里的导航（设置菜单、列表）。当前项：一块淡墨色底；悬停：更淡的一层墨色。" avoid="不要用和侧栏底色相同的颜色做悬停色——这就是设置页现在“没有悬停反馈”的原因。">
        <Demo surface="sunken">
          <div className="w-56 space-y-4">
            <NavItem icon={Settings2} label="返回工作区" onClick={() => undefined} />
            <NavSection title="设置">
              {[['general', '通用', Settings2], ['appearance', '外观', Palette], ['roles', '角色', Smile], ['archived', '已归档', Archive]].map(([id, label, Icon]) => (
                <NavItem key={id as string} icon={Icon as typeof Settings2} label={label as string} current={nav === id} onClick={() => setNav(id as string)} />
              ))}
            </NavSection>
          </div>
        </Demo>
        <Demo title="状态对照（静态示意），放在侧栏底色上" surface="sunken">
          <div className="flex flex-wrap gap-4">
            {[
              ['静止', ''],
              ['悬停', shownState.hover],
              ['按下', shownState.pressed],
              ['键盘焦点', shownState.focus],
            ].map(([label, cls]) => (
              <div key={label} className="w-40 space-y-1">
                <NavItem icon={Palette} label="外观" className={cls} onClick={() => undefined} />
                <p className="px-2.5 text-2xs text-neutral-500">{label}</p>
              </div>
            ))}
            <div className="w-40 space-y-1">
              <NavItem icon={Palette} label="外观" current onClick={() => undefined} />
              <p className="px-2.5 text-2xs text-neutral-500">当前</p>
            </div>
          </div>
        </Demo>
      </Doc>

      <Doc id="tabs" name="Tabs 标签页" file="components/ui/Navigation.tsx" use="在同一个对象的几个视图之间切换（工作面板：任务、审批、动态、记忆）。下划线样式用于面板顶部；胶囊样式用于内容里的筛选。" a11y="左右方向键切换，Home/End 到两端；用 TabPanel 包住内容。">
        <Demo surface="raised">
          <Tabs idPrefix="demo-tabs" label="工作面板" value={tab} onChange={setTab} tabs={[{ id: 'tasks', label: '任务', count: 3 }, { id: 'approvals', label: '审批' }, { id: 'activity', label: '动态' }, { id: 'memory', label: '记忆' }]} />
          <TabPanel idPrefix="demo-tabs" value={tab} className="pt-3 text-sm text-neutral-500">当前：{tab}</TabPanel>
          <div className="mt-6">
            <Tabs idPrefix="demo-pills" variant="pill" label="任务筛选" value={pill} onChange={setPill} tabs={[{ id: 'all', label: '全部' }, { id: 'open', label: '进行中', count: 2 }, { id: 'done', label: '已完成' }]} />
          </div>
        </Demo>
      </Doc>

      <Doc id="tree" name="Tree 树" file="components/ui/Tree.tsx" use="可以展开的层级（团队和成员）。" a11y="上下键移动；右键展开或进入；左键收起或回到上一级；回车选择。">
        <Demo surface="sunken">
          <div className="w-64">
            <Tree
              label="团队"
              selected={node}
              onSelect={setNode}
              defaultExpanded={['team-a']}
              nodes={[
                { id: 'team-a', label: '待办应用', children: [{ id: 'lead', label: '项目经理' }, { id: 'fe', label: '全栈工程师' }, { id: 'qa', label: '测试工程师' }] },
                { id: 'team-b', label: '官网改版', children: [{ id: 'lead-b', label: '项目经理' }] },
              ]}
            />
          </div>
        </Demo>
      </Doc>

      <Doc id="breadcrumbs" name="Breadcrumbs 面包屑 · Steps 步骤 · Pagination 分页" file="components/ui/Navigation.tsx" use="面包屑：页面在哪一层。步骤：一个短流程走到哪一步。分页：很长的列表（短列表用“加载更多”）。">
        <Demo surface="raised">
          <div className="space-y-5">
            <Breadcrumbs label="位置" items={[{ label: '设置', onClick: () => undefined }, { label: '已归档', onClick: () => undefined }, { label: '官网改版' }]} />
            <Steps label="新建团队" current={1} steps={['选择目录', '选择成员', '完成']} />
            <Pagination label="分页" page={page} pages={12} onChange={setPage} previousLabel="上一页" nextLabel="下一页" />
          </div>
        </Demo>
      </Doc>

      <Doc id="menu" name="Menu 操作菜单" file="components/ui/Overlay.tsx" use="对一个对象的一组操作（团队的“…”菜单）。危险操作放最后，用分隔线隔开。几个互斥的模式（权限模式）也用菜单：每项给 checked，可带一行说明，底部可放一句备注；触发按钮用 ComposerOption。" avoid="导航用 NavItem；选择一个值用 Select 或 Combobox。" a11y="打开后焦点在第一项；上下键移动，回车执行，Esc 关闭并回到按钮。">
        <Demo>
          <div className="flex items-center gap-3">
            <Menu
              label="团队操作"
              trigger={<IconButton icon={MoreHorizontal} label="团队操作" />}
              placement="bottom-start"
              items={[
                { id: 'rename', label: '重命名', icon: Pencil, onSelect: () => setLast('重命名') },
                { id: 'copy', label: '复制目录路径', icon: Copy, shortcut: 'Ctrl C', onSelect: () => setLast('复制目录路径') },
                { id: 'archive', label: '归档', icon: Archive, onSelect: () => setLast('归档') },
                'separator',
                { id: 'delete', label: '删除团队', icon: Trash2, danger: true, onSelect: () => setLast('删除团队') },
              ]}
            />
            <Menu
              label="权限模式"
              trigger={<ComposerOption icon={ShieldCheck}>{mode}</ComposerOption>}
              placement="bottom-start"
              note="从每个成员的下一轮开始生效。"
              items={MODES.map((entry) => ({
                id: entry.label,
                label: entry.label,
                description: entry.description,
                checked: mode === entry.label,
                onSelect: () => setMode(entry.label),
              }))}
            />
            {last && <span className="text-xs text-neutral-500">刚才选了：{last}</span>}
          </div>
        </Demo>
      </Doc>

      <Doc id="popover" name="Popover 气泡面板 · Tooltip 提示" file="components/ui/Overlay.tsx" use="Popover：点开的小面板，放几项控件或明细（会话统计）。Tooltip：悬停或聚焦时的几个字。" avoid="不要把只有在 Tooltip 里才能看到的重要信息放进 Tooltip；不要在 Tooltip 里放可点的东西。">
        <Demo>
          <div className="flex flex-wrap items-center gap-4">
            <Popover label="用量明细" trigger={<Button size="sm">15 tok · 缓存命中 0%</Button>}>
              <p className="font-semibold">Token 用量</p>
              <dl className="mt-2 space-y-1 text-xs">
                {[['输入', '12'], ['缓存读取', '0'], ['输出', '3']].map(([k, v]) => (
                  <div key={k} className="flex justify-between">
                    <dt className="text-neutral-500">{k}</dt>
                    <dd className="font-mono tabular-nums">{v}</dd>
                  </div>
                ))}
              </dl>
            </Popover>
            <Tooltip content="上下文已用 3%">
              <Button size="sm" variant="ghost">悬停我</Button>
            </Tooltip>
          </div>
        </Demo>
      </Doc>

      <Doc id="dialog" name="Dialog 对话框 · AlertDialog 确认框" file="components/ui/Overlay.tsx" use="Dialog：需要先完成再回到页面的事（新建团队、更换头像）。内容超过窗口时标题和按钮不动，中间区域自己滚动；需要更矮的上限时传 maxHeight（如 32rem），它不会超过窗口能给的高度。AlertDialog：做不可撤销的事之前确认，确认按钮写出动作本身；要先选一项才能做（删除哪个成员）时，把选择放进 children，用 confirmDisabled 等选好再放开。" avoid="不要用对话框显示只需要看一眼的消息（用 Toast 或 Alert）。" a11y="打开时焦点进入、Tab 在内部循环；Esc 和点遮罩关闭（处理中时不能关）；关闭后焦点回到打开它的按钮。">
        <Demo>
          <Matrix
            items={[
              { label: 'Dialog', node: <Button onClick={() => setDialog(true)}>打开对话框</Button> },
              { label: 'AlertDialog', node: <Button variant="danger" onClick={() => setAlert(true)}>删除团队</Button> },
            ]}
          />
          <Dialog
            open={dialog}
            onClose={() => setDialog(false)}
            title="重命名团队"
            description="成员会在团队列表里看到新名字。"
            closeLabel="关闭"
            footer={
              <>
                <Button variant="ghost" onClick={() => setDialog(false)}>取消</Button>
                <Button variant="primary" onClick={() => setDialog(false)}>保存名称</Button>
              </>
            }
          >
            <Field label="团队名称">
              <TextInput defaultValue="待办应用" />
            </Field>
          </Dialog>
          <AlertDialog open={alert} onCancel={() => setAlert(false)} onConfirm={() => setAlert(false)} title="删除“官网改版”？" description="团队的对话、任务和记忆都会删除，不能恢复。工作目录里的文件不受影响。" confirmLabel="删除团队" cancelLabel="取消" />
        </Demo>
      </Doc>

      <Doc id="sheet" name="Sheet 侧滑面板" file="components/ui/Overlay.tsx" use="从侧边滑出、需要先处理完再回到页面的面板（比如一份长的详情）。" avoid="团队列表和工作面板不用 Sheet：它们始终在布局里，可以收起，但打开时不能盖住会话、不能挡住继续操作。" a11y="和对话框一样管理焦点；Esc 关闭。">
        <Demo>
          <Matrix
            items={[
              { label: '从左侧', node: <Button onClick={() => setSheet('left')}>团队列表</Button> },
              { label: '从右侧', node: <Button onClick={() => setSheet('right')}>工作面板</Button> },
            ]}
          />
          <Sheet open={sheet !== null} side={sheet ?? 'right'} onClose={() => setSheet(null)} title={sheet === 'left' ? '团队列表' : '工作面板'} closeLabel="关闭">
            <p className="text-sm text-neutral-500">面板内容</p>
          </Sheet>
        </Demo>
      </Doc>

      <Doc id="workspace" name="TabStrip 工作区标签条 · SplitHandle 调宽线 · FloatingCard 悬浮信息卡" file="components/ui/{TabStrip,SplitHandle,FloatingCard}.tsx" use="工作区（会话右侧的标签区）：TabStrip 是可关闭、可滚动的标签条，选中的标签与内容同为白底；SplitHandle 是会话与工作区之间的调宽线；FloatingCard 是浮在会话角落的信息卡，打开时内容为它让位。" a11y="标签条按 tablist 语义：←→ 切换、Home/End 到两端、Delete 关闭；调宽线是 separator，左右方向键调整（Shift 加大步长）；悬浮卡有 region 标签，Esc 收起。">
        <Demo surface="sunken">
          <div className="flex h-64 w-full">
            <div className="min-w-0 flex-1">
              <TabStrip
                tabs={wsTabs}
                activeId={wsActive}
                label="工作区标签页"
                closeTabLabel="关闭"
                onSelect={setWsActive}
                onClose={(id) => {
                  const next = wsTabs.filter((tab) => tab.id !== id);
                  setWsTabs(next);
                  if (wsActive === id) setWsActive(next[next.length - 1]?.id ?? '');
                }}
                onNew={() => {
                  if (!wsTabs.some((tab) => tab.id === 'new')) setWsTabs([...wsTabs, { id: 'new', label: '新标签页' }]);
                  setWsActive('new');
                }}
                newTabLabel="新建标签页"
                onCollapse={() => setLast('工作区已收起（标签保留）')}
                collapseLabel="收起工作区"
              />
              <p className="px-2 pt-3 text-xs text-neutral-500 dark:text-neutral-400">当前宽度 {wsWidth}px · {last || '内容区'}</p>
            </div>
            <SplitHandle value={wsWidth} min={380} max={1200} label="工作区宽度" onChange={setWsWidth} />
            {card && (
              <FloatingCard label="信息" closeLabel="收起" onClose={() => setCard(false)} className="ml-2">
                <p className="px-3 py-2 text-sm">任务、审批、动态、记忆四页放在这里；Esc 收起。</p>
              </FloatingCard>
            )}
          </div>
          <div className="mt-3">
            <Button size="sm" variant={card ? 'secondary' : 'ghost'} onClick={() => setCard((open) => !open)} aria-expanded={card}>
              {card ? '收起信息卡' : '打开信息卡'}
            </Button>
          </div>
        </Demo>
      </Doc>
    </Chapter>
  );
}
