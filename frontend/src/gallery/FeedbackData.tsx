// Gallery: feedback, data display and page structure.
import { useState } from 'react';
import { Inbox, Plus } from 'lucide-react';
import { Accordion, Alert, Badge, Button, Card, CodeBlock, CopyButton, Count, DescriptionList, Divider, EmptyState, Kbd, List, ListRow, PageHeader, PanelHeader, PanelToggle, ProgressBar, ProgressRing, Section, Skeleton, Spinner, Stat, StatusDot, Table, Tag, Toolbar, useToast } from '../components/ui';
import { Chapter, Demo, Doc, Matrix } from './parts';

interface UsageRow {
  member: string;
  turns: number;
  tokens: number;
}

const ROWS: UsageRow[] = [
  { member: '项目经理', turns: 4, tokens: 182400 },
  { member: '全栈工程师', turns: 1, tokens: 96210 },
  { member: '测试工程师', turns: 2, tokens: 51877 },
];

export default function FeedbackData() {
  const toast = useToast();
  const [sort, setSort] = useState<{ key: string; direction: 'asc' | 'desc' }>({ key: 'tokens', direction: 'desc' });
  const [tags, setTags] = useState(['进行中', '全栈工程师']);
  const [leftOpen, setLeftOpen] = useState(true);
  const rows = [...ROWS].sort((a, b) => {
    const k = sort.key as keyof UsageRow;
    const order = a[k] > b[k] ? 1 : a[k] < b[k] ? -1 : 0;
    return sort.direction === 'asc' ? order : -order;
  });

  return (
    <Chapter id="feedback" title="反馈、数据与结构" intro="告诉人发生了什么、正在发生什么、接下来做什么；把数据摆清楚；把页面分好区。">
      <Doc
        id="toast"
        name="Toast 消息"
        file="components/ui/Toast.tsx"
        use="一件事完成了，但结果不在眼前（已保存、已复制、已撤销）。从窗口顶部居中落下；成功的 2.5 秒后自动消失，失败的一直留着，直到点关闭。最多同时三条。"
        avoid="和某个字段有关的错误写在字段下面；需要用户立刻决定的事用对话框。"
        a11y="成功用 polite、失败用 assertive 读出；鼠标停在上面时不会消失。"
      >
        <Demo>
          <div className="flex flex-wrap gap-2">
            <Button onClick={() => toast.show({ title: '已保存' })}>成功</Button>
            <Button onClick={() => toast.show({ tone: 'info', title: '头像库已是最新', description: '团子：第 1 版' })}>信息</Button>
            <Button onClick={() => toast.show({ tone: 'warning', title: '已归档“官网改版”', action: { label: '撤销', onClick: () => toast.show({ title: '已恢复' }) } })}>带操作</Button>
            <Button onClick={() => toast.show({ tone: 'danger', title: '没能保存', description: '连不上 CoBot 核心。检查它是否还在运行，然后再试一次。' })}>失败</Button>
          </div>
        </Demo>
      </Doc>

      <Doc id="alert" name="Alert 提示条" file="components/ui/Feedback.tsx" use="页面里需要读到的情况：引擎未登录、目录丢失、额度快用完。它是页面内容的一部分，放在相关内容的上方（比如会话顶部、设置分组顶部），不浮起、不自动消失。写清发生了什么和怎么办，最多带一个操作。" avoid="一闪而过的结果用 Toast；需要马上决定的用对话框。">
        <Demo>
          <div className="space-y-3">
            <Alert tone="info" title="头像库还没下载">成员暂时显示默认头像。可以到“设置 → 外观”检查更新。</Alert>
            <Alert tone="success">Codex 已登录，安全钩子已授权。</Alert>
            <Alert tone="warning" title="本周额度已用 79%" action={<Button size="sm">查看用量</Button>}>额度用完后成员会暂停，直到下周重置。</Alert>
            <Alert tone="danger" title="团队目录不见了" onClose={() => undefined} closeLabel="关闭">E:\work\todo-app 已经不存在。团队暂时只读；恢复目录，或在设置里归档这个团队。</Alert>
          </div>
        </Demo>
      </Doc>

      <Doc id="progress" name="Spinner · ProgressBar · Skeleton" file="components/ui/Feedback.tsx" use="等待时给出反馈：很短的等待用 Spinner；知道进度用 ProgressBar；内容加载用和内容同形状的 Skeleton。" avoid="不要整页转圈；能先显示的部分先显示。">
        <Demo>
          <div className="grid gap-6 md:grid-cols-3">
            <div className="space-y-3">
              <Spinner label="正在检查更新…" />
              <Spinner />
            </div>
            <div className="space-y-3">
              <ProgressBar label="上下文已用" value={62} />
              <ProgressBar label="正在下载" tone="success" value={30} />
              <ProgressBar label="正在准备" />
            </div>
            <div className="flex items-center gap-3">
              <Skeleton className="h-9 w-9 rounded-lg" />
              <div className="flex-1 space-y-2">
                <Skeleton className="h-3 w-2/3" />
                <Skeleton className="h-3 w-1/3" />
              </div>
            </div>
          </div>
        </Demo>
      </Doc>

      <Doc id="progress-ring" name="ProgressRing 环形进度" file="components/ui/Feedback.tsx" use="一个比例需要一眼看出时（上下文已用多少、额度用了多少），放在统计栏或卡片角上。" a11y="以 progressbar 读出百分比。">
        <Demo>
          <Matrix
            items={[
              { label: '上下文 23%', node: <ProgressRing value={23} label="上下文已用" /> },
              { label: '额度 79%（警告）', node: <ProgressRing value={79} label="本周额度" tone="warning" /> },
              { label: '超限（错误）', node: <ProgressRing value={100} label="预算" tone="danger" /> },
              { label: '小号，不显示数字', node: <ProgressRing value={45} label="上下文已用" size={16} showValue={false} /> },
            ]}
          />
        </Demo>
      </Doc>

      <Doc id="empty" name="EmptyState 空状态" file="components/ui/Feedback.tsx" use="一个地方暂时没有内容：说明为什么空、下一步做什么，最多一个主要操作。">
        <Demo surface="raised">
          <EmptyState icon={<Inbox size={28} />} title="还没有任务" description="和项目经理对齐方案后，任务会出现在这里。" action={<Button variant="primary" size="sm" icon={Plus}>新建任务</Button>} />
        </Demo>
      </Doc>

      <Doc id="badge" name="Badge · Tag · StatusDot · Count · Kbd" file="components/ui/Feedback.tsx" use="Badge：状态或分类。Tag：可移除的值。StatusDot：状态点，必须有文字标签。Count：数量。Kbd：快捷键。">
        <Demo>
          <div className="space-y-4">
            <Matrix
              items={[
                { label: 'neutral', node: <Badge>已归档</Badge> },
                { label: 'info', node: <Badge tone="info" dot>排队中</Badge> },
                { label: 'success', node: <Badge tone="success" dot>工作中</Badge> },
                { label: 'warning', node: <Badge tone="warning" dot>等你审批</Badge> },
                { label: 'danger', node: <Badge tone="danger">已中断</Badge> },
              ]}
            />
            <Matrix
              items={[
                { label: 'Tag', node: <span className="flex gap-1.5">{tags.map((tag) => <Tag key={tag} removeLabel={`移除 ${tag}`} onRemove={() => setTags(tags.filter((t) => t !== tag))}>{tag}</Tag>)}</span> },
                { label: 'StatusDot', node: <span className="flex gap-2"><StatusDot tone="success" label="工作中" pulse /><StatusDot tone="warning" label="等你审批" /><StatusDot tone="danger" label="已中断" /></span> },
                { label: 'Count', node: <span className="flex gap-2"><Count value={3} label="3 个任务" /><Count value={120} tone="danger" label="120 条" /></span> },
                { label: 'Kbd', node: <span className="flex items-center gap-1 text-xs"><Kbd>Enter</Kbd>发送<Kbd>Shift</Kbd>+<Kbd>Enter</Kbd>换行</span> },
              ]}
            />
          </div>
        </Demo>
      </Doc>

      <Doc id="card" name="Card 卡片 · Divider 分隔" file="components/ui/Display.tsx" use="一组相关内容，比如设置里的一个分组。卡片在页面里用描边，不用阴影。">
        <Demo>
          <div className="grid gap-4 md:grid-cols-2">
            <Card title="头像库" description="成员头像从这里来。" action={<Button size="sm">检查更新</Button>}>
              <p className="text-sm">团子：第 1 版</p>
            </Card>
            <Card>
              <p className="text-sm">没有标题的卡片</p>
              <div className="my-4">
                <Divider label="或者" />
              </div>
              <p className="text-sm">分隔线可以带文字</p>
            </Card>
          </div>
        </Demo>
      </Doc>

      <Doc id="list" name="List 列表" file="components/ui/Display.tsx" use="一列同类的东西（角色、已归档团队）。行可以整体可点，也可以只有尾部操作。">
        <Demo surface="raised">
          <List label="角色">
            <ListRow leading={<span className="h-9 w-9 rounded-lg bg-[#9cc8fb]" />} title="全栈工程师" description="默认头像" trailing={<Button size="sm">更换头像</Button>} />
            <ListRow leading={<span className="h-9 w-9 rounded-lg bg-[#fbb271]" />} title="测试工程师" description="已自定义" onClick={() => undefined} selected />
            <ListRow leading={<span className="h-9 w-9 rounded-lg bg-[#d8b1fb]" />} title="产品经理" description="可点的行" onClick={() => undefined} />
          </List>
        </Demo>
      </Doc>

      <Doc id="table" name="Table 表格" file="components/ui/Display.tsx" use="按同样几列比较多行数据（用量、历史）。数字列右对齐、等宽。" a11y="可排序的列标头是按钮，并带 aria-sort。">
        <Demo>
          <Table<UsageRow>
            caption="各成员用量"
            rowKey={(row) => row.member}
            rows={rows}
            sort={sort}
            onSort={(key) => setSort((s) => ({ key, direction: s.key === key && s.direction === 'desc' ? 'asc' : 'desc' }))}
            columns={[
              { key: 'member', header: '成员', cell: (row) => row.member, sortable: true },
              { key: 'turns', header: '轮数', cell: (row) => row.turns, numeric: true, sortable: true },
              { key: 'tokens', header: 'Token', cell: (row) => row.tokens.toLocaleString(), numeric: true, sortable: true },
            ]}
          />
        </Demo>
      </Doc>

      <Doc id="description-list" name="DescriptionList 键值列表 · Stat 指标 · CodeBlock 代码" file="components/ui/Display.tsx" use="键值列表：一组属性。指标：一个重要的数字。代码块：命令、输出、配置，带复制。">
        <Demo surface="raised">
          <div className="grid gap-6 lg:grid-cols-2">
            <DescriptionList
              items={[
                { term: '版本', value: '0.1.0' },
                { term: '数据位置', value: 'C:\\Users\\me\\.cobot', mono: true },
                { term: 'Codex', value: '0.161.0 · 已登录' },
              ]}
            />
            <div className="flex gap-8">
              <Stat label="本周额度" value="79%" note="周一 08:00 重置" />
              <Stat label="缓存命中" value="95%" />
            </div>
            <div className="lg:col-span-2">
              <CodeBlock label="PowerShell" copyLabel="复制" copiedLabel="已复制">{'cobot serve --profile production'}</CodeBlock>
            </div>
          </div>
        </Demo>
      </Doc>

      <Doc id="accordion" name="Accordion 折叠面板 · CopyButton 复制按钮" file="components/ui/Display.tsx" use="折叠面板：可以分别展开的几段内容（任务的交付记录、高级设置）。复制按钮：一个值旁边的复制，点后图标短暂变成对勾、读屏器读出“已复制”。">
        <Demo surface="raised">
          <div className="space-y-4">
            <Accordion
              defaultOpen={['delivery']}
              items={[
                { id: 'delivery', title: '交付记录', meta: '2 次', content: '全栈工程师 09:52 交付：添加和删除可用，本地保存已完成，已用 Playwright 验证。' },
                { id: 'criteria', title: '验收标准', content: '刷新后数据仍在；空输入不能添加；删除需要确认。' },
                { id: 'advanced', title: '高级设置', content: '协作轮次上限、预算等。' },
              ]}
            />
            <div className="flex items-center gap-2 font-mono text-xs">
              C:\Users\me\.cobot
              <CopyButton value="C:\Users\me\.cobot" copyLabel="复制路径" copiedLabel="已复制" />
            </div>
          </div>
        </Demo>
      </Doc>

      <Doc id="structure" name="PageHeader · Section · PanelHeader · Toolbar · PanelToggle" file="components/ui/Layout.tsx" use="页面和面板的骨架。PanelToggle：侧栏的打开和收起永远是同一个按钮、同一个位置、同一对图标。" avoid="不要用 × 关闭侧栏、再用另一个图标打开它；不要为打开按钮单独留一条空列。">
        <Demo surface="raised">
          <div className="space-y-6">
            <PageHeader eyebrow="设置" title="外观" description="浅色、深色或跟随系统，以及界面配色。" actions={<Button size="sm">恢复默认</Button>} />
            <Section title="头像库" description="成员头像从这里来。">
              <Toolbar label="头像库操作">
                <Button size="sm">检查更新</Button>
                <Button size="sm" variant="ghost">查看许可证</Button>
              </Toolbar>
            </Section>
            <div className="overflow-hidden rounded-xl border border-neutral-200 dark:border-neutral-800">
              <PanelHeader
                leading={<PanelToggle side="left" open={leftOpen} onToggle={() => setLeftOpen(!leftOpen)} openLabel="展开团队列表" closeLabel="收起团队列表" />}
                title="项目经理"
                subtitle="待办应用 · E:\work\todo-app"
                actions={<PanelToggle side="right" open onToggle={() => undefined} openLabel="展开工作面板" closeLabel="收起工作面板" />}
              />
              <p className="p-4 text-xs text-neutral-500">团队列表：{leftOpen ? '展开' : '收起'}</p>
            </div>
          </div>
        </Demo>
      </Doc>
    </Chapter>
  );
}
