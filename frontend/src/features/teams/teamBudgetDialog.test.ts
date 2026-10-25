// A team's budget dialog: each row follows the default, sets its own limit,
// or has none, and says what was spent today. A member stays under its team.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n';
import TeamBudgetDialog from './TeamBudgetDialog';

const state = vi.hoisted(() => ({
  budget: null as unknown,
  profile: 'production',
  setTeam: vi.fn(),
  setBot: vi.fn(),
  resetTeam: vi.fn(),
  resetBot: vi.fn(),
}));

vi.mock('../../api/queries', () => ({
  useTeamBudget: () => ({ data: state.budget, isPending: false, isError: false }),
  useStatus: () => ({ data: { profile: state.profile } }),
  useSetTeamBudget: () => ({ mutate: state.setTeam, isPending: false }),
  useSetBotBudget: () => ({ mutate: state.setBot, isPending: false }),
  useResetTeamBudget: () => ({ mutate: state.resetTeam, isPending: false }),
  useResetBotBudget: () => ({ mutate: state.resetBot, isPending: false }),
}));
vi.mock('../../components/ui/Toast', () => ({ useToast: () => ({ show: vi.fn(), dismiss: vi.fn() }) }));

const item = {
  team: { id: 't1', name: '示例团队' },
  bots: [
    { id: 'b1', name: '项目经理' },
    { id: 'b2', name: '全栈工程师' },
  ],
} as never;

const line = (over: Record<string, unknown>) => ({ override: null, default: 0, effective: 0, limitedBy: 'none', ceiling: 0, usedToday: 0, ...over });

describe('team budget dialog', () => {
  let root: Root;
  let host: HTMLDivElement;

  const render = (budget: unknown) => {
    state.budget = budget;
    act(() => root.render(h(TeamBudgetDialog, { item, onClose: vi.fn() })));
  };

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
    for (const mock of [state.setTeam, state.setBot, state.resetTeam, state.resetBot]) mock.mockReset();
  });

  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  const radio = (row: number, name: string) =>
    [...[...host.querySelectorAll('[role="radiogroup"]')][row].querySelectorAll<HTMLButtonElement>('[role="radio"]')].find((button) =>
      button.textContent?.includes(name),
    );
  const buttonByText = (text: string, index = 0) => [...host.querySelectorAll('button')].filter((button) => button.textContent?.includes(text))[index];

  const ownTeamLimit = () =>
    render({
      team: line({ default: 100_000_000, effective: 100_000_000, limitedBy: 'default', usedToday: 30_000_000 }),
      members: [
        { botId: 'b1', ...line({ override: 0, effective: 100_000_000, limitedBy: 'team', ceiling: 100_000_000, usedToday: 5_000_000 }) },
        { botId: 'b2', ...line({ override: 20_000_000, effective: 20_000_000, limitedBy: 'own', ceiling: 100_000_000 }) },
      ],
    });

  it('says what each row follows and how much of it was spent today', () => {
    ownTeamLimit();
    expect(host.textContent).toContain('跟随默认（1 亿）');
    expect(host.textContent).toContain('今日已用 3000 万 / 1 亿');
  });

  it('sets the team its own limit starting from the default, and lets it have none', () => {
    ownTeamLimit();
    act(() => radio(0, '自定义')!.click());
    expect(state.setTeam).toHaveBeenCalledWith(100_000_000, expect.anything());
    act(() => radio(0, '不限制')!.click());
    expect(state.setTeam).toHaveBeenLastCalledWith(0, expect.anything());
  });

  it('does not let a member be unlimited while the team has a limit', () => {
    ownTeamLimit();
    const none = radio(1, '不单独限制')!;
    expect(none.disabled).toBe(true);
    expect(host.textContent).toContain('团队有上限时，成员不能设为不限制');
  });

  it('holds a member to its team: the number field stops at the team limit', () => {
    ownTeamLimit();
    // The second member has its own limit of 2 千万; the field may reach 10 千万 and no more.
    const input = host.querySelectorAll<HTMLInputElement>('input[type="number"]')[0];
    expect(input.max).toBe('10');
    expect(input.value).toBe('2');
  });

  it('says a member without a limit of its own is held to the team, sharing it', () => {
    ownTeamLimit();
    expect(host.textContent).toContain('受团队上限限制，实际最多 1 亿，且与其他成员共用');
  });

  it('lets the members be unlimited when the team has no limit', () => {
    render({
      team: line({}),
      members: [{ botId: 'b1', ...line({ usedToday: 1 }) }, { botId: 'b2', ...line({}) }],
    });
    const none = radio(1, '不单独限制')!;
    expect(none.disabled).toBe(false);
    act(() => none.click());
    expect(state.setBot).toHaveBeenCalledWith({ botId: 'b1', limit: 0 }, expect.anything());
  });

  it('lets a member follow the default again', () => {
    ownTeamLimit();
    act(() => radio(1, '跟随默认')!.click());
    expect(state.setBot).toHaveBeenCalledWith({ botId: 'b1', limit: null }, expect.anything());
  });

  it('resets today\'s count for the team, which resets its members too, and for one member', () => {
    ownTeamLimit();
    expect(host.textContent).toContain('团队重置会同时重置所有成员');
    act(() => buttonByText('重置今日用量', 0)!.click());
    expect(state.resetTeam).toHaveBeenCalledTimes(1);
    act(() => buttonByText('重置今日用量', 1)!.click());
    expect(state.resetBot).toHaveBeenCalledWith('b1', expect.anything());
  });

  it('has nothing to reset on a row that spent nothing', () => {
    ownTeamLimit();
    expect(buttonByText('重置今日用量', 2)!.disabled).toBe(true);
  });
});
