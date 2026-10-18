// Gallery: patterns that span components, writing rules and what not to do.
import { useState } from 'react';
import { PanelLeftOpen, X } from 'lucide-react';
import { Alert, Button, Field, FieldError, NavItem, PanelToggle, Select, Switch, TextInput, useToast } from '../components/ui';
import { Chapter, Demo, Doc, DoDont, Rules } from './parts';

export default function Patterns() {
  const toast = useToast();
  const [language, setLanguage] = useState('zh-CN');
  const [check, setCheck] = useState(true);
  const [path, setPath] = useState('E:\\work');
  const [listOpen, setListOpen] = useState(true);

  return (
    <Chapter id="patterns" title="交互模式、文案与禁止事项" intro="跨组件的约定。新界面先在这里找做法；这里没有的，先补进画廊再用。">
      <Doc id="saving" name="保存反馈" file="useToast + Field" use="设置项改了就保存。成功：窗口顶部居中弹出 Toast“已保存”。失败：错误写在那个设置项下面，值回到保存前，同时给一条失败 Toast。" avoid="不要在每个设置项旁边显示“已保存”。">
        <Demo surface="raised">
          <div className="grid max-w-xl gap-5">
            <Field label="界面语言">
              <Select
                value={language}
                onChange={(next) => {
                  setLanguage(next);
                  toast.show({ title: '已保存' });
                }}
                options={[{ value: 'zh-CN', label: '简体中文' }, { value: 'en', label: 'English' }]}
              />
            </Field>
            <Switch checked={check} onChange={(value) => { setCheck(value); toast.show({ title: '已保存' }); }} label="启动时检查头像库更新" />
            <Field label="引擎路径" error={path.trim() === '' ? '路径不能为空。填写 codex 可执行文件的完整路径。' : undefined}>
              <TextInput mono value={path} onChange={(event) => setPath(event.target.value)} />
            </Field>
          </div>
        </Demo>
      </Doc>

      <Doc id="panels" name="侧栏的展开与收起" file="PanelToggle" use="左右两栏的开关固定在会话标题栏的两端，用同一对“侧栏”图标表示开和关。宽屏时侧栏在页面里；窄屏时用 Sheet 从侧边滑出。">
        <Demo>
          <div className="flex h-40 overflow-hidden rounded-xl border border-neutral-200 dark:border-neutral-800">
            {listOpen && <div className="w-40 border-r border-neutral-200 bg-neutral-100 p-3 text-xs text-neutral-500 dark:border-neutral-800 dark:bg-neutral-900">团队列表</div>}
            <div className="flex-1">
              <div className="flex items-center gap-2 border-b border-neutral-200 px-2 py-2 dark:border-neutral-800">
                <PanelToggle side="left" open={listOpen} onToggle={() => setListOpen(!listOpen)} openLabel="展开团队列表" closeLabel="收起团队列表" />
                <span className="flex-1 text-sm font-semibold">队长</span>
                <PanelToggle side="right" open={false} onToggle={() => undefined} openLabel="展开工作面板" closeLabel="收起工作面板" />
              </div>
            </div>
          </div>
        </Demo>
      </Doc>

      <Doc id="states" name="加载、空、出错" file="Skeleton · EmptyState · Alert" use="加载：先显示骨架，能先显示的先显示。空：说明为什么空、下一步做什么。出错：说清发生了什么、怎么恢复，并给一个重试操作。">
        <Demo>
          <Alert tone="danger" title="任务没有加载出来" action={<Button size="sm">重试</Button>}>连不上 CoBot 核心。确认它还在运行后重试。</Alert>
        </Demo>
      </Doc>

      <Doc id="writing" name="文案规则" file="frontend/src/i18n/locales/*.json" use="界面上的每个字。">
        <Rules
          items={[
            '从用户的角度说话：说用户认识的东西（“团队”“成员”“工作目录”），不说实现（“线程”“迁移”“端点”）。',
            '按钮写出动作：“保存修改”“删除团队”“更换头像”；确认框的确认按钮和动作同名。',
            '出错说清三件事：发生了什么、为什么（知道的话）、怎么办。不道歉，不含糊，不只写错误码。',
            '空状态是邀请：告诉用户下一步，最多一个主要操作。',
            '一句话做一件事；能删的字删掉。',
            '中文里夹英文、数字时不加空格，用全角标点；英文用句首大写（Sentence case）。',
            '所有文字都放进 i18n 两个语言包，键名一致；组件里不写死中文或英文，图标按钮的标签也要翻译。',
          ]}
        />
      </Doc>

      <Doc id="donts" name="禁止事项" file="—" use="下面每一对都来自真实踩过的坑。">
        <DoDont
          goodNote="悬停用半透明墨色叠层（interactive.hover），放在任何底色上都看得见。"
          badNote="悬停色和底色都是 neutral-100：鼠标移上去什么都看不到。"
          good={
            <div className="w-48 space-y-px rounded-lg bg-neutral-100 p-2 dark:bg-neutral-900">
              <NavItem label="通用" onClick={() => undefined} />
              <NavItem label="外观" current onClick={() => undefined} />
            </div>
          }
          bad={
            <div className="w-48 space-y-px rounded-lg bg-neutral-100 p-2 dark:bg-neutral-900">
              <button type="button" className="block w-full rounded-md px-2 py-1.5 text-left text-sm hover:bg-neutral-100">通用</button>
              <button type="button" className="block w-full rounded-md bg-neutral-200 px-2 py-1.5 text-left text-sm dark:bg-neutral-800">外观</button>
            </div>
          }
        />
        <DoDont
          goodNote="同一个按钮在同一个位置切换开和关，图标成对。"
          badNote="用 × 关闭，再用另一个图标、在另一个位置打开。"
          good={<PanelToggle side="left" open onToggle={() => undefined} openLabel="展开团队列表" closeLabel="收起团队列表" />}
          bad={
            <div className="flex items-center gap-6 text-neutral-500">
              <X size={18} aria-hidden />
              <PanelLeftOpen size={18} aria-hidden />
            </div>
          }
        />
        <DoDont
          goodNote="保存成功用 Toast；字段下面只放错误。"
          badNote="每个字段旁边一行绿色“已保存”。"
          good={<Field label="语言"><Select value="zh" onChange={() => undefined} options={[{ value: 'zh', label: '简体中文' }]} /></Field>}
          bad={
            <div className="space-y-1.5">
              <div className="flex justify-between text-sm"><span>语言</span><span className="text-xs text-green-600">已保存</span></div>
              <Select value="zh" onChange={() => undefined} options={[{ value: 'zh', label: '简体中文' }]} />
            </div>
          }
        />
        <DoDont
          goodNote="错误写清原因和办法，挨着出错的地方。"
          badNote="只写“操作失败”或一个错误码。"
          good={<FieldError>这个目录已经被“设计预览”团队使用了，换一个目录，或先归档那个团队。</FieldError>}
          bad={<p className="text-xs text-red-600">error.team.directory_bound</p>}
        />
      </Doc>
    </Chapter>
  );
}
