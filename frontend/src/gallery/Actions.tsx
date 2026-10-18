// Gallery: actions and form controls.
import { useState } from 'react';
import { ChevronDown, Filter, FolderOpen, Moon, Monitor, Plus, Save, Sun, Trash2 } from 'lucide-react';
import { Button, ButtonGroup, Checkbox, Combobox, Field, Fieldset, FileDrop, IconButton, NumberInput, RadioCards, RadioGroup, SearchInput, SegmentedControl, Select, Slider, Switch, TextInput, Textarea, Toggle } from '../components/ui';
import { Chapter, Demo, Doc, Matrix } from './parts';

export default function Actions() {
  const [checked, setChecked] = useState(true);
  const [switchOn, setSwitchOn] = useState(true);
  const [radio, setRadio] = useState('default');
  const [card, setCard] = useState('graphite');
  const [mode, setMode] = useState('system');
  const [toggle, setToggle] = useState(true);
  const [search, setSearch] = useState('');
  const [count, setCount] = useState(6);
  const [slider, setSlider] = useState(40);
  const [model, setModel] = useState('gpt-5.5');
  const [name, setName] = useState('');
  const [notes, setNotes] = useState('');
  const [file, setFile] = useState('');

  return (
    <Chapter id="actions" title="操作与表单" intro="按钮触发动作；表单控件改变一个值。设置类的控件改了就生效（保存成功用 Toast 告知），对话框里的表单点按钮才提交。">
      <Doc
        id="button"
        name="Button 按钮"
        file="components/ui/Button.tsx"
        use={<>触发一个动作。文字写动作本身（“保存修改”“新建团队”），不写“确定”“提交”。一个视图只放一个 primary。</>}
        avoid={<>跳转到别处用链接或导航项；不可撤销的操作才用 danger，并且要先弹 AlertDialog 确认。</>}
        a11y="loading 时自动禁用并标记 aria-busy；禁用的按钮仍保持可读的对比度。"
      >
        <Demo title="变体">
          <Matrix
            items={[
              { label: 'primary 主要', node: <Button variant="primary" icon={Plus}>新建团队</Button> },
              { label: 'secondary 次要（默认）', node: <Button icon={FolderOpen}>选择目录</Button> },
              { label: 'ghost 文字', node: <Button variant="ghost">取消</Button> },
              { label: 'danger 危险', node: <Button variant="danger" icon={Trash2}>删除团队</Button> },
              { label: 'link 链接', node: <Button variant="link">查看详情</Button> },
            ]}
          />
        </Demo>
        <Demo title="状态（悬停和按下请直接在页面上试）">
          <Matrix
            items={[
              { label: '静止', node: <Button variant="primary">保存修改</Button> },
              { label: '加载中', node: <Button variant="primary" loading>保存中</Button> },
              { label: '禁用', node: <Button variant="primary" disabled>保存修改</Button> },
              { label: '次要 · 禁用', node: <Button disabled>选择目录</Button> },
              { label: '带后置图标', node: <Button trailingIcon={ChevronDown}>更多</Button> },
            ]}
          />
        </Demo>
        <Demo title="尺寸：sm 28 · md 36（默认）· lg 44">
          <Matrix
            items={[
              { label: 'sm', node: <Button size="sm" variant="primary">保存</Button> },
              { label: 'md', node: <Button variant="primary">保存</Button> },
              { label: 'lg', node: <Button size="lg" variant="primary">保存</Button> },
              { label: 'block 占满一行', node: <div className="w-48"><Button block variant="primary" icon={Save}>保存修改</Button></div> },
            ]}
          />
        </Demo>
      </Doc>

      <Doc id="icon-button" name="IconButton 图标按钮" file="components/ui/Button.tsx" use="空间有限、图标含义足够明确的操作：收起面板、关闭、更多。" avoid="含义不明显的操作（用带文字的按钮）。" a11y="label 必填，同时作为读屏名称和悬停提示；开关类用 pressed。">
        <Demo>
          <Matrix
            items={[
              { label: 'ghost（默认）', node: <IconButton icon={Filter} label="筛选" /> },
              { label: 'secondary', node: <IconButton icon={Filter} label="筛选" variant="secondary" /> },
              { label: 'primary', node: <IconButton icon={Plus} label="新建" variant="primary" /> },
              { label: 'pressed 开启', node: <IconButton icon={Filter} label="筛选" pressed /> },
              { label: 'disabled', node: <IconButton icon={Filter} label="筛选" disabled /> },
              { label: 'sm', node: <IconButton icon={Filter} label="筛选" size="sm" /> },
            ]}
          />
        </Demo>
      </Doc>

      <Doc id="button-group" name="ButtonGroup 按钮组" file="components/ui/Button.tsx" use="几个并列且相关的操作。" avoid="互斥的选择（用 SegmentedControl）。">
        <Demo>
          <ButtonGroup label="排序">
            <Button size="sm">按名称</Button>
            <Button size="sm">按时间</Button>
            <Button size="sm">按状态</Button>
          </ButtonGroup>
        </Demo>
      </Doc>

      <Doc id="field" name="Field 字段" file="components/ui/Field.tsx" use="给每个输入控件配标签、提示和错误。里面的控件自动关联 id 和说明。" a11y="错误用 role=alert 读出，并通过 aria-describedby 关联到控件。" avoid="不要在字段旁边放“已保存”——保存成功用 Toast。">
        <Demo surface="raised">
          <div className="grid max-w-xl gap-5">
            <Field label="团队名称" hint="成员会在团队列表里看到这个名字。" required>
              <TextInput value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：待办应用" />
            </Field>
            <Field label="工作目录" error="这个目录已经被“设计预览”团队使用了，换一个目录，或先归档那个团队。">
              <TextInput mono value="E:\\work\\todo-app" readOnly />
            </Field>
          </div>
        </Demo>
      </Doc>

      <Doc id="text-input" name="TextInput · Textarea · SearchInput · NumberInput" file="components/ui/Input.tsx" use="输入文字和数字。路径、ID、密钥用 mono。数字有上下限时用 NumberInput。" a11y="SearchInput 按 Esc 清空；NumberInput 的加减按钮必须传入文字标签。">
        <Demo surface="raised">
          <div className="grid gap-5 md:grid-cols-2">
            <Field label="普通">
              <TextInput placeholder="输入文字" />
            </Field>
            <Field label="带前后缀">
              <TextInput prefix={<span className="text-xs">https://</span>} suffix={<span className="text-xs">/assets</span>} placeholder="example.com" />
            </Field>
            <Field label="禁用">
              <TextInput disabled value="不能修改" readOnly />
            </Field>
            <Field label="小尺寸">
              <TextInput size="sm" placeholder="sm" />
            </Field>
            <Field label="搜索">
              <SearchInput label="搜索团队" clearLabel="清除" value={search} onChange={setSearch} placeholder="搜索团队名称或目录" />
            </Field>
            <Field label="成员上限" hint="1–20">
              <NumberInput value={count} onChange={setCount} min={1} max={20} unit="人" decreaseLabel="减少" increaseLabel="增加" />
            </Field>
            <Field label="多行（随内容变高）" className="md:col-span-2">
              <Textarea value={notes} onChange={(event) => setNotes(event.target.value)} autoGrowMax={160} placeholder="写下补充要求……" />
            </Field>
          </div>
        </Demo>
      </Doc>

      <Doc id="select" name="Select 下拉 · Combobox 可搜索下拉" file="components/ui/Input.tsx · Combobox.tsx" use="Select：选项少且固定（语言）。Combobox：选项多、需要打字筛选（模型、角色）。" avoid="2–4 个选项且要一眼看全时用 SegmentedControl 或 RadioGroup。" a11y="Combobox：上下键移动、回车选择、Esc 关闭。">
        <Demo surface="raised">
          <div className="grid gap-5 md:grid-cols-2">
            <Field label="界面语言">
              <Select options={[{ value: 'zh-CN', label: '简体中文' }, { value: 'en', label: 'English' }]} defaultValue="zh-CN" />
            </Field>
            <Field label="默认模型">
              <Combobox
                value={model}
                onChange={setModel}
                emptyText="没有匹配的模型"
                options={[
                  { value: 'gpt-5.5', label: 'gpt-5.5', description: '默认，综合能力最好' },
                  { value: 'gpt-5.5-mini', label: 'gpt-5.5-mini', description: '更快、更省' },
                  { value: 'gpt-5.5-codex', label: 'gpt-5.5-codex', description: '为编程调优' },
                  { value: 'o5', label: 'o5', description: '深度推理' },
                ]}
              />
            </Field>
          </div>
        </Demo>
      </Doc>

      <Doc id="checkbox" name="Checkbox 复选 · Switch 开关" file="components/ui/Choice.tsx" use="Checkbox：表单里的独立是/否，点保存才生效（建团队时勾选成员）。Switch：一个设置，拨动立即生效。" avoid="不要用 Switch 表示需要再点保存的选择。">
        <Demo surface="raised">
          <div className="grid gap-5 md:grid-cols-2">
            <div className="space-y-3">
              <Checkbox checked={checked} onChange={setChecked} label="前端开发" description="实现界面与交互。" />
              <Checkbox checked={false} onChange={() => undefined} label="产品经理" />
              <Checkbox checked={false} indeterminate onChange={() => undefined} label="全部成员（部分选中）" />
              <Checkbox checked onChange={() => undefined} disabled label="队长（必选）" />
            </div>
            <div className="space-y-4">
              <Switch checked={switchOn} onChange={setSwitchOn} label="启动时检查头像库更新" description="关闭后只在点“检查更新”时下载。" />
              <Switch checked={false} onChange={() => undefined} disabled label="禁用的开关" />
            </div>
          </div>
        </Demo>
      </Doc>

      <Doc id="radio" name="RadioGroup 单选 · RadioCards 单选卡片" file="components/ui/Choice.tsx" use="从几个选项里选一个，而且要全部摆出来对比。需要图片或说明时用卡片。" a11y="方向键在选项间移动并选中，和原生单选一致。">
        <Demo surface="raised">
          <div className="grid gap-6 md:grid-cols-2">
            <RadioGroup
              label="权限模式"
              value={radio}
              onChange={setRadio}
              options={[
                { value: 'read_only', label: '只读', description: '成员只能查看，不能改文件。' },
                { value: 'default', label: '默认权限', description: '改文件、运行命令前请你批准。' },
                { value: 'full', label: '完全访问', description: '不再询问。切换时需要确认。' },
              ]}
            />
            <RadioCards
              label="主题"
              columns={2}
              value={card}
              onChange={setCard}
              options={[
                { value: 'graphite', label: '石墨', description: '带一点绿意的石墨灰' },
                { value: 'harbor', label: '港湾', description: '偏冷的蓝灰' },
                { value: 'dusk', label: '暮梅', description: '带紫调的暖灰' },
                { value: 'none', label: '不可选', description: '禁用状态', disabled: true },
              ]}
            />
          </div>
        </Demo>
      </Doc>

      <Doc id="segmented" name="SegmentedControl 分段选择 · Toggle 切换按钮" file="components/ui/Choice.tsx" use="SegmentedControl：2–4 个短选项，立即生效（明暗模式、视图切换）。Toggle：一个保持开或关的按钮（筛选）。">
        <Demo>
          <div className="flex flex-wrap items-center gap-6">
            <SegmentedControl
              label="明暗"
              value={mode}
              onChange={setMode}
              options={[
                { value: 'system', label: '跟随系统', icon: Monitor },
                { value: 'light', label: '浅色', icon: Sun },
                { value: 'dark', label: '深色', icon: Moon },
              ]}
            />
            <Toggle pressed={toggle} onChange={setToggle} icon={Filter}>只看进行中</Toggle>
          </div>
        </Demo>
      </Doc>

      <Doc id="slider" name="Slider 滑块" file="components/ui/Slider.tsx" use="位置比精确数字更重要的值。需要精确输入时配一个 NumberInput。">
        <Demo surface="raised">
          <div className="max-w-sm">
            <Slider label="预算提醒阈值" value={slider} onChange={setSlider} format={(value) => `${value}%`} />
          </div>
        </Demo>
      </Doc>

      <Doc id="file-drop" name="FileDrop 文件选择" file="components/ui/FileDrop.tsx" use="从本机选一个文件，可以点也可以拖进来。写清接受哪些格式。">
        <Demo surface="raised">
          <FileDrop accept="image/png,image/jpeg,image/webp" title="选择图片，或拖到这里" hint="PNG、JPEG 或 WebP，会自动裁成正方形" onFile={(picked) => setFile(picked.name)} />
          {file && <p className="mt-2 text-xs text-neutral-500">已选择：{file}</p>}
        </Demo>
      </Doc>

      <Doc id="fieldset" name="Fieldset 字段组" file="components/ui/Field.tsx" use="把相关的字段归成一组，加一个小标题。">
        <Demo surface="raised">
          <Fieldset legend="协作与预算" description="控制团队规模和用量。">
            <Field label="成员上限">
              <NumberInput value={count} onChange={setCount} min={1} max={20} decreaseLabel="减少" increaseLabel="增加" />
            </Field>
          </Fieldset>
        </Demo>
      </Doc>
    </Chapter>
  );
}
