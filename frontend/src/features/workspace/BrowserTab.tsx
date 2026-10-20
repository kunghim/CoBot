// BrowserTab is one page of the team's browser in the workspace: a real,
// shared page, not a mirror. What the user clicks or types goes into the
// same tab the member is using. The page renders at its real viewport and is
// scaled to fit the workspace (or shown at 100%); the size the page really
// has is a page property the user and every member share.
import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { sendBrowserInput, streamBrowserTab, type BrowserFrame, type BrowserInput } from '../../api/client';
import { useSetBrowserTabViewport, useTeamBrowser } from '../../api/queries';
import type { Bot } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import { Alert, EmptyState, Icons, IconButton, Select, TextInput } from '../../components/ui';

const MOVE_INTERVAL_MS = 40;

function modifiers(e: { altKey: boolean; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }): number {
  return (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0);
}

/** How the picture is shown: scaled into the workspace, or at 100%. */
type Scale = 'fit' | 'full';

export default function BrowserTab({ teamId, panelTabId, bots, onClose }: { teamId: string; panelTabId: string; bots: Bot[]; onClose: () => void }) {
  const { t } = useTranslation();
  const state = useTeamBrowser(teamId);
  const setViewport = useSetBrowserTabViewport(teamId);
  const page = state.data?.tabs.find((item) => item.id === panelTabId);
  const gone = state.isSuccess && state.data !== undefined && !page;

  const canvas = useRef<HTMLCanvasElement>(null);
  const boxRef = useRef<HTMLDivElement>(null);
  const frameSize = useRef({ width: 1280, height: 900 });
  const [box, setBox] = useState({ width: 0, height: 0 });
  const [error, setError] = useState<string | null>(null);
  const [address, setAddress] = useState('');
  const [editing, setEditing] = useState(false);
  const [hasFrame, setHasFrame] = useState(false);
  const [scale, setScale] = useState<Scale>('fit');
  const [viewportError, setViewportError] = useState<string | null>(null);

  // The workspace box, for fitting the picture inside it.
  useEffect(() => {
    const target = boxRef.current;
    if (!target) return;
    const observer = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect;
      setBox({ width, height });
    });
    observer.observe(target);
    return () => observer.disconnect();
  }, []);

  // Inputs go out one at a time and in order; a mouse move waiting behind
  // others is dropped, since the next one supersedes it.
  const queue = useRef<Promise<void>>(Promise.resolve());
  const pendingMoves = useRef(0);
  const send = useCallback(
    (input: BrowserInput) => {
      if (!panelTabId) return;
      const move = input.kind === 'mouse' && input.action === 'move';
      if (move) {
        if (pendingMoves.current > 0) return;
        pendingMoves.current++;
      }
      queue.current = queue.current
        .then(() => sendBrowserInput(teamId, panelTabId, input))
        .catch((err) => setError(apiErrorMessage(t, err)))
        .finally(() => {
          if (move) pendingMoves.current--;
        });
    },
    [teamId, panelTabId, t],
  );

  // The pictures of this page. Only the visible tab mounts this component,
  // so a background tab never streams.
  useEffect(() => {
    if (!panelTabId) return;
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
            panelTabId,
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
  }, [teamId, panelTabId, t]);

  // The address bar follows the page unless the user is typing in it.
  useEffect(() => {
    if (!editing) setAddress(page?.url ?? '');
  }, [page?.url, editing]);

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
      const scaleToFrame = frameSize.current.width / rect.width;
      send({
        kind: 'wheel', x: ((e.clientX - rect.left) / rect.width) * frameSize.current.width, y: ((e.clientY - rect.top) / rect.height) * frameSize.current.height,
        dx: e.deltaX * scaleToFrame, dy: e.deltaY * scaleToFrame, modifiers: modifiers(e),
      });
    };
    target.addEventListener('wheel', onWheel, { passive: false });
    return () => target.removeEventListener('wheel', onWheel);
  }, [send, panelTabId, hasFrame]);

  if (state.data && !state.data.available) {
    return <EmptyState icon={<Icons.openExternal size={20} aria-hidden />} title={t('browser.notInstalled')} description={t('browser.notInstalledHint')} />;
  }
  if (gone) {
    // The member closed its page (or the browser stopped): say so and let
    // the tab be closed in one click.
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3">
        <EmptyState icon={<Icons.openExternal size={20} aria-hidden />} title={t('workspace.browser.closed')} />
        <button
          type="button"
          onClick={onClose}
          className="rounded-lg bg-neutral-900/[0.06] px-3 py-1.5 text-xs font-medium text-neutral-700 hover:bg-neutral-900/[0.1] dark:bg-white/[0.08] dark:text-neutral-200 dark:hover:bg-white/[0.12]"
        >
          {t('workspace.browser.closeClosed')}
        </button>
      </div>
    );
  }

  // Fit the picture into the box, or show it at 100% inside a scroller.
  const natural = frameSize.current;
  const fitScale = box.width > 0 && box.height > 0 ? Math.min(box.width / natural.width, box.height / natural.height) : 1;
  const shown = scale === 'fit' ? fitScale : 1;
  const shownWidth = Math.round(natural.width * shown);
  const shownHeight = Math.round(natural.height * shown);
  const percent = Math.round(shown * 100);

  const memberName = (id: string) => bots.find((bot) => bot.id === id)?.name ?? id;
  const isUser = page?.owner === 'user';
  const statusLabel = isUser
    ? t('workspace.browser.userOwn')
    : t('workspace.browser.memberTab', { name: memberName(page?.memberId ?? '') });
  const statusHint = isUser ? t('workspace.browser.userHint') : t('workspace.browser.memberHint');

  // The real window size the page has; the dropdown changes it for everyone.
  const viewportValue = (() => {
    if (scale === 'fit') return 'fit';
    if (scale === 'full') return 'full';
    return '';
  })();
  const applyViewport = (value: string) => {
    if (value === 'fit' || value === 'full') {
      setScale(value);
      return;
    }
    setViewportError(null);
    setViewport.mutate(
      { tab: panelTabId, body: { preset: value as 'desktop' | 'tablet' | 'phone' } },
      { onError: (err) => setViewportError(apiErrorMessage(t, err)) },
    );
  };

  return (
    <div className="flex h-full min-h-0 flex-col gap-1.5 px-2 pb-2 pt-1.5">
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
      <div className="flex items-center gap-2 px-0.5">
        <p className="flex min-w-0 flex-1 items-center gap-1.5 truncate text-2xs text-neutral-500 dark:text-neutral-400">
          {page?.busy && <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" aria-hidden />}
          <span className="truncate font-medium text-neutral-700 dark:text-neutral-300">{statusLabel}</span>
          {page?.busy && <span className="shrink-0 rounded bg-emerald-100 px-1 py-px text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200">{t('workspace.browser.memberBusy')}</span>}
          <span className="truncate">· {statusHint}</span>
        </p>
        <span className="shrink-0 font-mono text-2xs tabular-nums text-neutral-500 dark:text-neutral-400">
          {natural.width} × {natural.height} · {percent}%
        </span>
        <Select
          size="sm"
          label={t('workspace.browser.viewport')}
          value={viewportValue}
          onChange={applyViewport}
          options={[
            { value: 'fit', label: t('workspace.browser.viewportFit') },
            { value: 'full', label: t('workspace.browser.viewportFull') },
            { value: 'desktop', label: t('workspace.browser.viewportDesktop') },
            { value: 'tablet', label: t('workspace.browser.viewportTablet') },
            { value: 'phone', label: t('workspace.browser.viewportPhone') },
          ]}
        />
      </div>
      {(error || viewportError) && <Alert tone="warning">{[error, viewportError].filter(Boolean).join(' · ')}</Alert>}
      <div
        ref={boxRef}
        className={`relative min-h-0 flex-1 overflow-hidden rounded-lg bg-neutral-100 ring-1 ring-inset ring-neutral-200 dark:bg-neutral-900 dark:ring-neutral-800 ${scale === 'full' ? 'overflow-auto' : ''}`}
      >
        <div className={scale === 'fit' ? 'flex h-full w-full items-center justify-center' : 'inline-block'}>
          <canvas
            ref={canvas}
            tabIndex={0}
            role="application"
            aria-label={t('browser.view')}
            style={{ width: shownWidth || undefined, height: shownHeight || undefined }}
            className="block max-w-none cursor-default bg-white outline-none focus-visible:ring-2 focus-visible:ring-signal-500"
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
        </div>
        {!hasFrame && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-sm text-neutral-500 dark:text-neutral-400">
            {t('browser.loading')}
          </div>
        )}
      </div>
    </div>
  );
}
