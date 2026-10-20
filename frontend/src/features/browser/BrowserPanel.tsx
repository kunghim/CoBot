// BrowserPanel is the "浏览器" page of the right panel: the team's browser as
// it works, live. It is a real, shared browser, not a mirror: what the user
// clicks or types goes into the same tab the member is using, and nothing is
// paused for it. A member is told when the user touched its tab.
import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { sendBrowserInput, streamBrowserTab, type BrowserFrame, type BrowserInput } from '../../api/client';
import { useTeamBrowser } from '../../api/queries';
import type { Bot } from '../../api/types';
import { useShell } from '../../app/shell';
import { apiErrorMessage } from '../../i18n/errors';
import { Alert, EmptyState, Icons, IconButton, Select, TextInput } from '../../components/ui';

const MOVE_INTERVAL_MS = 40;

function modifiers(e: { altKey: boolean; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }): number {
  return (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0);
}

export default function BrowserPanel({ teamId, bots }: { teamId: string; bots: Bot[] }) {
  const { t } = useTranslation();
  const state = useTeamBrowser(teamId);
  const { setPanelWide } = useShell();
  useEffect(() => {
    setPanelWide(true);
    return () => setPanelWide(false);
  }, [setPanelWide]);
  const tabs = state.data?.tabs ?? [];
  const [selected, setSelected] = useState('');
  const tab = tabs.find((item) => item.id === selected) ?? tabs[0];
  const tabId = tab?.id ?? '';
  const canvas = useRef<HTMLCanvasElement>(null);
  const frameSize = useRef({ width: 1280, height: 900 });
  const [error, setError] = useState<string | null>(null);
  const [address, setAddress] = useState('');
  const [editing, setEditing] = useState(false);
  const [hasFrame, setHasFrame] = useState(false);

  // Inputs go out one at a time and in order; a mouse move waiting behind
  // others is dropped, since the next one supersedes it.
  const queue = useRef<Promise<void>>(Promise.resolve());
  const pendingMoves = useRef(0);
  const send = useCallback(
    (input: BrowserInput) => {
      if (!tabId) return;
      const move = input.kind === 'mouse' && input.action === 'move';
      if (move) {
        if (pendingMoves.current > 0) return;
        pendingMoves.current++;
      }
      queue.current = queue.current
        .then(() => sendBrowserInput(teamId, tabId, input))
        .catch((err) => setError(apiErrorMessage(t, err)))
        .finally(() => {
          if (move) pendingMoves.current--;
        });
    },
    [teamId, tabId, t],
  );

  // The pictures of the selected tab.
  useEffect(() => {
    if (!tabId) return;
    setHasFrame(false);
    const controller = new AbortController();
    let decoding = false;
    let latest: BrowserFrame | null = null;
    const draw = () => {
      if (decoding || !latest) return;
      const frame = latest;
      latest = null;
      decoding = true;
      const image = new Image();
      image.onload = () => {
        const target = canvas.current;
        if (target) {
          if (target.width !== image.naturalWidth || target.height !== image.naturalHeight) {
            target.width = image.naturalWidth;
            target.height = image.naturalHeight;
          }
          target.getContext('2d')?.drawImage(image, 0, 0);
          frameSize.current = { width: frame.width || image.naturalWidth, height: frame.height || image.naturalHeight };
          setHasFrame(true);
        }
        decoding = false;
        draw();
      };
      image.onerror = () => {
        decoding = false;
        draw();
      };
      image.src = 'data:image/jpeg;base64,' + frame.data;
    };
    const follow = async () => {
      while (!controller.signal.aborted) {
        try {
          const ended = await streamBrowserTab(
            teamId,
            tabId,
            (frame) => {
              latest = frame;
              draw();
            },
            controller.signal,
          );
          setError(ended);
        } catch (err) {
          if (controller.signal.aborted) return;
          setError(apiErrorMessage(t, err));
        }
        await new Promise((resolve) => setTimeout(resolve, 2000));
      }
    };
    void follow();
    return () => controller.abort();
  }, [teamId, tabId, t]);

  // The address bar follows the page unless the user is typing in it.
  useEffect(() => {
    if (!editing) setAddress(tab?.url ?? '');
  }, [tab?.url, editing]);

  const point = (e: { clientX: number; clientY: number }) => {
    const rect = canvas.current!.getBoundingClientRect();
    return {
      x: ((e.clientX - rect.left) / rect.width) * frameSize.current.width,
      y: ((e.clientY - rect.top) / rect.height) * frameSize.current.height,
    };
  };
  const buttonName = (button: number) => (button === 2 ? 'right' : button === 1 ? 'middle' : 'left');
  const lastMove = useRef(0);

  const onPointerDown = (e: PointerEvent<HTMLCanvasElement>) => {
    e.currentTarget.focus();
    e.currentTarget.setPointerCapture(e.pointerId);
    send({ kind: 'mouse', action: 'down', ...point(e), button: buttonName(e.button), clickCount: e.detail || 1, modifiers: modifiers(e) });
  };
  const onPointerUp = (e: PointerEvent<HTMLCanvasElement>) => {
    send({ kind: 'mouse', action: 'up', ...point(e), button: buttonName(e.button), clickCount: e.detail || 1, modifiers: modifiers(e) });
  };
  const onPointerMove = (e: PointerEvent<HTMLCanvasElement>) => {
    const now = performance.now();
    if (now - lastMove.current < MOVE_INTERVAL_MS) return;
    lastMove.current = now;
    send({ kind: 'mouse', action: 'move', ...point(e), modifiers: modifiers(e) });
  };
  const onKey = (e: KeyboardEvent<HTMLCanvasElement>, action: 'down' | 'up') => {
    // Paste arrives as its own event so the clipboard stays the user's.
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'v') return;
    e.preventDefault();
    const printable = e.key.length === 1 && !e.ctrlKey && !e.metaKey;
    send({
      kind: 'key', action, key: e.key, code: e.code, vk: e.keyCode, modifiers: modifiers(e),
      text: action === 'down' ? (printable ? e.key : e.key === 'Enter' ? '\r' : undefined) : undefined,
    });
  };

  // Wheel needs a non-passive listener to keep the page from scrolling.
  useEffect(() => {
    const target = canvas.current;
    if (!target) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const rect = target.getBoundingClientRect();
      const scale = frameSize.current.width / rect.width;
      send({
        kind: 'wheel', x: ((e.clientX - rect.left) / rect.width) * frameSize.current.width, y: ((e.clientY - rect.top) / rect.height) * frameSize.current.height,
        dx: e.deltaX * scale, dy: e.deltaY * scale, modifiers: modifiers(e),
      });
    };
    target.addEventListener('wheel', onWheel, { passive: false });
    return () => target.removeEventListener('wheel', onWheel);
  }, [send, tabId, hasFrame]);

  if (state.isLoading) return <div className="px-4 py-6 text-sm text-neutral-500">{t('browser.loading')}</div>;
  if (state.data && !state.data.available) {
    return <EmptyState icon={<Icons.openExternal size={20} aria-hidden />} title={t('browser.notInstalled')} description={t('browser.notInstalledHint')} />;
  }
  if (!tabs.length) {
    return <EmptyState icon={<Icons.openExternal size={20} aria-hidden />} title={t('browser.idle')} description={t('browser.idleHint')} />;
  }

  const memberName = (id: string) => bots.find((bot) => bot.id === id)?.name ?? id;
  const options = tabs.map((item) => ({ value: item.id, label: `${memberName(item.memberId)} · ${item.title || item.url || t('browser.blank')}` }));

  return (
    <div className="flex h-full min-h-0 flex-col gap-2 px-3 pb-3">
      <Select label={t('browser.tabs')} options={options} value={tabId} onChange={setSelected} size="sm" />
      <div className="flex items-center gap-1">
        <IconButton label={t('browser.back')} icon={Icons.previous} size="sm" onClick={() => send({ kind: 'nav', to: 'back' })} />
        <IconButton label={t('browser.forward')} icon={Icons.next} size="sm" onClick={() => send({ kind: 'nav', to: 'forward' })} />
        <IconButton label={t('browser.reload')} icon={Icons.refresh} size="sm" onClick={() => send({ kind: 'nav', to: 'reload' })} />
        <form
          className="min-w-0 flex-1"
          onSubmit={(e) => {
            e.preventDefault();
            if (address.trim()) send({ kind: 'nav', to: address.trim() });
            setEditing(false);
          }}
        >
          <TextInput
            size="sm"
            mono
            aria-label={t('browser.address')}
            value={address}
            onChange={(e) => setAddress(e.target.value)}
            onFocus={() => setEditing(true)}
            onBlur={() => setEditing(false)}
          />
        </form>
      </div>
      {error && <Alert tone="warning">{error}</Alert>}
      <div className="relative min-h-0 flex-1 overflow-hidden rounded-lg bg-neutral-100 ring-1 ring-inset ring-neutral-200 dark:bg-neutral-900 dark:ring-neutral-800">
        <canvas
          ref={canvas}
          tabIndex={0}
          role="application"
          aria-label={t('browser.view')}
          className="block h-auto max-h-full w-full cursor-default outline-none focus-visible:ring-2 focus-visible:ring-signal-500"
          onPointerDown={onPointerDown}
          onPointerUp={onPointerUp}
          onPointerMove={onPointerMove}
          onContextMenu={(e) => e.preventDefault()}
          onKeyDown={(e) => onKey(e, 'down')}
          onKeyUp={(e) => onKey(e, 'up')}
          onPaste={(e) => {
            e.preventDefault();
            const text = e.clipboardData.getData('text');
            if (text) send({ kind: 'text', text });
          }}
        />
        {!hasFrame && <div className="absolute inset-0 flex items-center justify-center text-sm text-neutral-500">{t('browser.loading')}</div>}
      </div>
      <p className="text-2xs leading-5 text-neutral-500 dark:text-neutral-400">{t('browser.sharedHint')}</p>
    </div>
  );
}
