// Gallery: members and conversations — CoBot's own components.
import { useState } from 'react';
import { Brain, ShieldCheck } from 'lucide-react';
import dangoSeries from '../../../assets/avatars/dango/dist/dango.json';
import type { AvatarSeries } from '../api/types';
import { AvatarImage } from '../features/avatars/Avatar';
import { FALLBACK_AVATAR, drawAvatar, roleAvatar, type AvatarState } from '../features/avatars/render';
import { AppTitleBar, ApprovalCard, AvatarStack, Badge, Button, Composer, ComposerOption, ExchangeLine, MemberRow, MessageBubble, PanelToggle, ProcessStep, QuestionCard, RoleChip, SystemNote } from '../components/ui';
import { Chapter, Demo, Doc, Matrix } from './parts';

const dango: AvatarSeries = { id: 'dango', name: dangoSeries.name, kind: 'composed', version: dangoSeries.version, data: dangoSeries as unknown as AvatarSeries['data'] };

function RoleAvatar({ roleId, size = 24, state = 'idle' }: { roleId: string; size?: number; state?: AvatarState }) {
  const avatar = roleAvatar(dango, roleId);
  return <AvatarImage src={avatar ? drawAvatar(dango, avatar, state) : FALLBACK_AVATAR} size={size} />;
}

const person = (roleId: string, name: string, size = 16) => ({ avatar: <RoleAvatar roleId={roleId} size={size} />, name });

export default function TeamConversation() {
  const [selected, setSelected] = useState('leader');
  const [draft, setDraft] = useState('');
  const [running, setRunning] = useState(false);
  const [card, setCard] = useState(1);
  const [answer, setAnswer] = useState('');
  const [left, setLeft] = useState(true);
  const [right, setRight] = useState(true);
  const [maximized, setMaximized] = useState(false);

  return (
    <Chapter id="team" title="成员与会话" intro="CoBot 自己的组件。成员 = 头像 + 名字，在哪里提到成员都一起出现；状态由头像表达（工作中会动、等你处理时睁大眼睛），同时用文字告诉读屏器。">
      <Doc id="avatar" name="Avatar 头像" file="features/avatars/Avatar.tsx · render.ts" use="所有显示成员的地方。尺寸：16（行内、往来行）、24（列表）、28（消息旁）、36（会话头部）、48（空状态）、96（选头像）。" avoid="不要自己画首字母方块；头像库没下载时组件会自动用打包的默认头像。" a11y="头像本身对读屏器隐藏，成员名和状态由旁边的文字或外层的标签说出。">
        <Demo>
          <div className="space-y-5">
            <Matrix items={[16, 24, 28, 36, 48, 96].map((size) => ({ label: `${size}px`, node: <RoleAvatar roleId="frontend-dev" size={size} /> }))} />
            <Matrix
              items={[
                { label: '空闲', node: <RoleAvatar roleId="qa-engineer" size={48} /> },
                { label: '工作中（会动）', node: <RoleAvatar roleId="qa-engineer" size={48} state="working" /> },
                { label: '等你处理', node: <RoleAvatar roleId="qa-engineer" size={48} state="waiting" /> },
                { label: '已中断', node: <RoleAvatar roleId="qa-engineer" size={48} state="stopped" /> },
                { label: '默认头像（未下载）', node: <AvatarImage src={FALLBACK_AVATAR} size={48} /> },
                { label: '本地图片 · 工作中', node: <span className="inline-block h-12 w-12 animate-pulse rounded-[28%] bg-gradient-to-br from-amber-300 to-rose-400 ring-2 ring-emerald-500" /> },
              ]}
            />
            <Matrix items={['leader', 'product-manager', 'frontend-dev', 'backend-dev', 'qa-engineer', 'code-reviewer'].map((roleId) => ({ label: roleId, node: <RoleAvatar roleId={roleId} size={36} /> }))} />
          </div>
        </Demo>
      </Doc>

      <Doc id="role-chip" name="RoleChip 角色名牌 · AvatarStack 头像叠放" file="components/ui/Team.tsx" use="名牌：只在没有位置放头像的紧凑场合用（任务负责人、表格单元格）。会话里提到成员时用“头像 + 名字”，不用名牌。叠放：一组成员一眼看过。">
        <Demo>
          <div className="flex flex-wrap items-center gap-4">
            <span className="flex flex-wrap gap-1.5">
              <RoleChip name="队长" isLeader />
              <RoleChip name="产品经理" roleId="product-manager" />
              <RoleChip name="前端开发" roleId="frontend-dev" />
              <RoleChip name="后端开发" roleId="backend-dev" />
              <RoleChip name="测试工程师" roleId="qa-engineer" />
              <RoleChip name="评审员" roleId="code-reviewer" />
            </span>
            <AvatarStack avatars={['leader', 'frontend-dev', 'backend-dev', 'qa-engineer'].map((roleId) => <RoleAvatar key={roleId} roleId={roleId} size={28} />)} more="+2" />
          </div>
        </Demo>
      </Doc>

      <Doc id="member-row" name="MemberRow 成员行" file="components/ui/Team.tsx" use="团队列表里的一个成员：头像、名字、右上角状态或时间、下面一行是最近一条消息。消息只显示一行，超出用省略号截断，不会撑出侧栏。当前成员白底加描边；等你处理时头像右下角加琥珀色点。">
        <Demo surface="sunken">
          <div className="w-64 space-y-px">
            {[
              ['leader', '队长', 'idle', '', '空闲', '方案已经发你了，确认后我就分派给成员。', '09:41'],
              ['frontend-dev', '前端开发', 'working', '工作中', '工作中', '正在写 src/components/TodoList.tsx，添加和删除已经能用，接下来补本地保存', ''],
              ['backend-dev', '后端开发', 'idle', '', '空闲', '', ''],
              ['qa-engineer', '测试工程师', 'waiting', '等你审批', '等你审批', '想在沙箱外运行 npx playwright test --headed 来验证页面', ''],
            ].map(([roleId, name, state, status, statusLabel, preview, time]) => (
              <MemberRow
                key={roleId}
                avatar={<RoleAvatar roleId={roleId} size={32} state={state as AvatarState} />}
                name={name}
                status={status || undefined}
                time={time || undefined}
                statusLabel={statusLabel}
                preview={preview || undefined}
                attention={state === 'waiting'}
                current={selected === roleId}
                onSelect={() => setSelected(roleId)}
              />
            ))}
          </div>
        </Demo>
      </Doc>

      <Doc id="messages" name="MessageBubble 消息 · ExchangeLine 往来 · SystemNote 系统说明" file="components/ui/Team.tsx" use="用户的话是右侧深色气泡；成员的回复在左侧，带头像和名字（纯文字，不加底色）；成员之间的往来是居中的一行，带对方头像，点名字打开完整往来；CoBot 自己的说明是居中的小字。">
        <Demo>
          <div className="mx-auto max-w-2xl space-y-4">
            <MessageBubble from="user" meta="已送达">做一个本地待办页面，能添加和删除</MessageBubble>
            <MessageBubble from="member" avatar={<RoleAvatar roleId="leader" size={28} />} author="队长">
              好的。我先确认两点：数据要不要在刷新后保留？要不要支持标记完成？
            </MessageBubble>
            <ExchangeLine label="发给" people={[person('frontend-dev', '前端开发'), person('qa-engineer', '测试工程师')]} onOpen={() => undefined} />
            <ExchangeLine label="来自" people={[person('qa-engineer', '测试工程师')]} onOpen={() => undefined} />
            <SystemNote>团队成员有变化：加入了评审员</SystemNote>
          </div>
        </Demo>
      </Doc>

      <Doc id="process" name="ProcessStep 过程步骤" file="components/ui/Team.tsx" use="成员一轮工作里做的每一步：命令、改文件、工具调用、子智能体……可以展开看详情。进行中转圈，完成打勾，失败打叉。">
        <Demo surface="raised">
          <div className="max-w-xl space-y-0.5">
            <ProcessStep kind="command" status="completed" title="npm test" duration="4.2s" detail={<pre className="font-mono">14 passed</pre>} />
            <ProcessStep kind="file_change" status="completed" title="修改 src/App.tsx、src/todo.ts" detail="2 个文件，+48 −3" />
            <ProcessStep kind="agent" status="in_progress" title="派生了子智能体：检查无障碍" badge={<Badge tone="info">子智能体</Badge>} />
            <ProcessStep kind="tool" status="failed" title="view_tasks" detail="连接中断，已重试" />
            <ProcessStep kind="diff" status="completed" title="本轮改动 3 个文件" />
          </div>
        </Demo>
      </Doc>

      <Doc
        id="approval"
        name="ApprovalCard 审批卡 · QuestionCard 提问卡"
        file="components/ui/Team.tsx（WaitingCard）"
        use="等你处理的事，放在输入框上方，一次一张，多张时可以翻页。中性底色的浮起卡片：左上角图标加标题，正文写清谁、要做什么、为什么，答复用胶囊按钮放在右下，最主要的放最右边。提问卡还带一个回答框。"
        avoid="不要用黄色等警告色铺满卡片；处理后卡片直接消失，不在消息流里留记录（历史在工作面板的“审批”页）。"
      >
        <Demo>
          <div className="mx-auto max-w-2xl space-y-4">
            <ApprovalCard
              title="安全审批"
              requester={person('qa-engineer', '测试工程师')}
              action="想在沙箱外运行命令"
              command="npx playwright test --headed"
              reason="需要打开真实浏览器验证页面。"
              position={{ index: card, total: 3 }}
              onPrevious={() => setCard(card - 1)}
              onNext={() => setCard(card + 1)}
              previousLabel="上一张"
              nextLabel="下一张"
              actions={
                <>
                  <Button size="sm" shape="pill" variant="ghost">拒绝并停止</Button>
                  <Button size="sm" shape="pill">拒绝</Button>
                  <Button size="sm" shape="pill">本会话都允许</Button>
                  <Button size="sm" shape="pill" variant="primary">允许</Button>
                </>
              }
            />
            <QuestionCard
              title="问题"
              asker={person('leader', '队长')}
              question="数据要不要在刷新后保留？要的话我会用浏览器本地存储。"
              answer={answer}
              onAnswer={setAnswer}
              placeholder="写下你的回答"
              previousLabel="上一个"
              nextLabel="下一个"
              onClose={() => undefined}
              closeLabel="稍后回答"
              actions={
                <>
                  <Button size="sm" shape="pill">跳过</Button>
                  <Button size="sm" shape="pill" variant="primary" disabled={answer.trim() === ''}>发送</Button>
                </>
              }
            />
          </div>
        </Demo>
      </Doc>

      <Doc id="composer" name="Composer 输入区" file="components/ui/Team.tsx" use="给成员写消息。Enter 发送，Shift+Enter 换行；成员工作中且没有草稿时，发送键变成停止键。左下角放团队级的选项。">
        <Demo>
          <div className="mx-auto max-w-2xl space-y-3">
            <Composer
              value={draft}
              onChange={setDraft}
              onSend={() => {
                setDraft('');
                setRunning(true);
              }}
              onStop={() => setRunning(false)}
              running={running}
              placeholder="给 队长 发消息……"
              sendLabel="发送"
              stopLabel="停止"
              options={
                <>
                  <ComposerOption icon={ShieldCheck}>默认权限</ComposerOption>
                  <ComposerOption icon={Brain}>跟随模型</ComposerOption>
                </>
              }
            />
            <p className="text-xs text-neutral-500">{running ? '工作中：输入框为空时显示停止键。' : '输入后按 Enter 试试。'}</p>
          </div>
        </Demo>
      </Doc>

      <Doc
        id="title-bar"
        name="AppTitleBar 桌面标题栏"
        file="components/ui/Layout.tsx"
        use="桌面客户端隐藏系统标题栏，由 CoBot 自己画：两端是左右侧栏的开关，中间可以拖动窗口、双击最大化，颜色跟着 CoBot 的主题和明暗。Windows 上右侧自己画最小化、最大化、关闭；macOS 左侧保留系统的红绿灯位置。"
        avoid="网页版没有标题栏，侧栏开关放在会话标题行的两端。"
        a11y="窗口按钮都有文字标签；关闭按钮悬停变红，和系统一致。"
      >
        <Demo title="Windows">
          <div className="overflow-hidden rounded-xl border border-neutral-200 dark:border-neutral-800">
            <AppTitleBar
              platform="windows"
              title="CoBot — 待办应用 · 队长"
              left={<PanelToggle side="left" open={left} onToggle={() => setLeft(!left)} openLabel="展开团队列表" closeLabel="收起团队列表" />}
              right={<PanelToggle side="right" open={right} onToggle={() => setRight(!right)} openLabel="展开工作面板" closeLabel="收起工作面板" />}
              windowControls={{ minimizeLabel: '最小化', maximizeLabel: '最大化', restoreLabel: '还原', closeLabel: '关闭', maximized, onMinimize: () => undefined, onToggleMaximize: () => setMaximized(!maximized), onClose: () => undefined }}
            />
            <div className="flex h-24 text-xs text-neutral-500">
              {left && <div className="w-36 border-r border-neutral-200 bg-neutral-100 p-3 dark:border-neutral-800 dark:bg-neutral-900">团队列表</div>}
              <div className="flex-1 p-3">会话</div>
              {right && <div className="w-36 border-l border-neutral-200 p-3 dark:border-neutral-800">工作面板</div>}
            </div>
          </div>
        </Demo>
        <Demo title="macOS">
          <div className="overflow-hidden rounded-xl border border-neutral-200 dark:border-neutral-800">
            <AppTitleBar
              platform="mac"
              title="CoBot — 待办应用 · 队长"
              left={<PanelToggle side="left" open={left} onToggle={() => setLeft(!left)} openLabel="展开团队列表" closeLabel="收起团队列表" />}
              right={<PanelToggle side="right" open={right} onToggle={() => setRight(!right)} openLabel="展开工作面板" closeLabel="收起工作面板" />}
            />
          </div>
        </Demo>
      </Doc>
    </Chapter>
  );
}
