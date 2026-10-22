import { act, createElement as h, StrictMode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Bot, Team } from '../../api/types';
import ReasoningEffortSelect from './ReasoningEffortSelect';

const state = vi.hoisted(() => ({ save: vi.fn(), toast: vi.fn() }));
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock('../../api/queries', () => ({
  useSettings: () => ({ data: { defaultModel: 'test-model' } }),
  useEngineModels: () => ({
    data: {
      models: [{
        id: 'test-model', isDefault: true, defaultReasoningEffort: 'medium',
        reasoningEfforts: [{ effort: 'low' }, { effort: 'medium' }, { effort: 'high' }],
      }],
    },
  }),
  useSetTeamReasoningEffort: () => ({ mutateAsync: state.save, isPending: false }),
  // The picker offers every enabled provider's models: here only the
  // ChatGPT account, signed in, with the engine's own list.
  useProviders: () => ({ data: { providers: [{ id: 'openai', name: 'OpenAI', kind: 'official', authMode: 'chatgpt', endpoints: [], models: [], enabled: true, revision: 1 }] } }),
  useProviderPresets: () => ({ data: { presets: [] } }),
  useEngine: () => ({ data: { status: 'ready', loggedIn: true } }),
  useSetBotModel: () => ({ mutate: vi.fn() }),
}));
vi.mock('../../components/ui/Toast', () => ({ useToast: () => ({ show: state.toast, dismiss: vi.fn() }) }));
vi.mock('../settings/useSettingSaver', () => ({ useSettingSaver: () => ({ save: vi.fn() }) }));

describe('thinking effort saves across view lifecycle changes', () => {
  let root: Root;
  let host: HTMLDivElement;
  let mounted: boolean;
  const team = { id: 'team-1', revision: 7, reasoningEffort: 'medium' } as Team;
  const bot = { id: 'bot-1', name: 'engineer', revision: 3 } as Bot;

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    vi.useFakeTimers();
    state.save.mockReset().mockResolvedValue({ revision: 8 });
    state.toast.mockReset();
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
    mounted = true;
  });

  const leave = () => {
    act(() => root.unmount());
    mounted = false;
  };
  afterEach(async () => {
    if (mounted) leave();
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    host.remove();
    vi.useRealTimers();
  });

  const open = (strict = false) => {
    const picker = h(ReasoningEffortSelect, { team, bot });
    act(() => root.render(strict ? h(StrictMode, null, picker) : picker));
    act(() => host.querySelector('button')!.click());
  };
  const choose = (key: 'Home' | 'End') => {
    const slider = host.querySelector('[role="slider"]')!;
    act(() => slider.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true })));
  };

  it('saves after StrictMode replays mount effects', async () => {
    open(true);
    choose('End');
    await act(async () => { await vi.advanceTimersByTimeAsync(400); });
    expect(state.save).toHaveBeenCalledExactlyOnceWith({ teamId: team.id, effort: 'high', expectedRevision: 7 });
  });

  it('submits a pending choice when the user leaves before the debounce ends', async () => {
    open();
    choose('End');
    expect(state.save).not.toHaveBeenCalled();
    leave();
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(state.save).toHaveBeenCalledExactlyOnceWith({ teamId: team.id, effort: 'high', expectedRevision: 7 });
  });

  it('finishes a queued choice after leaving without overlapping the active save', async () => {
    let finish!: (updated: { revision: number }) => void;
    state.save.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    open();
    choose('End');
    await act(async () => { await vi.advanceTimersByTimeAsync(400); });
    expect(state.save).toHaveBeenCalledTimes(1);
    choose('Home');
    leave();
    expect(state.save).toHaveBeenCalledTimes(1);
    await act(async () => { finish({ revision: 8 }); await vi.advanceTimersByTimeAsync(0); });
    expect(state.save).toHaveBeenCalledTimes(2);
    expect(state.save).toHaveBeenLastCalledWith({ teamId: team.id, effort: 'low', expectedRevision: 8 });
  });

  it('does not save or duplicate a choice merely because the view closes', async () => {
    open(true);
    leave();
    await act(async () => { await vi.advanceTimersByTimeAsync(500); });
    expect(state.save).not.toHaveBeenCalled();
  });
});
