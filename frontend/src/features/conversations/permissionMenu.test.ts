// The permission menu speaks of a team's folder and members, or of a chat's own
// folder, whichever it is in.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n';
import { PermissionModeMenu } from './PermissionModeSelect';

describe('PermissionModeMenu', () => {
  let root: Root;
  let host: HTMLDivElement;
  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
  });
  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  const askForFull = (kind: 'team' | 'chat') => {
    const onChange = vi.fn();
    act(() => root.render(h(PermissionModeMenu, { value: 'default', kind, onChange })));
    act(() => (document.querySelector('button[aria-label^="权限模式"]') as HTMLButtonElement).click());
    const item = [...document.querySelectorAll('[role="menuitemradio"],[role="menuitem"]')].find((entry) => entry.textContent?.includes('完全访问')) as HTMLElement;
    act(() => item.click());
    return { onChange, text: document.body.textContent ?? '' };
  };

  it('asks a team about its members and folder', () => {
    const { text, onChange } = askForFull('team');
    expect(text).toContain('从每位成员的下一轮开始生效');
    expect(onChange).not.toHaveBeenCalled();
  });

  it('asks a chat about its own folder and the next message, with no members in it', () => {
    const { text, onChange } = askForFull('chat');
    expect(text).toContain('聊天文件夹');
    expect(text).toContain('从你的下一条消息开始生效');
    expect(text).not.toContain('成员');
    expect(text).not.toContain('团队');
    expect(onChange).not.toHaveBeenCalled();
  });
});
