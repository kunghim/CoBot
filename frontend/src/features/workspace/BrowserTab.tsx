// BrowserTab is one page of the team's browser in the workspace: a real,
// shared page, not a mirror. What the user clicks or types goes into the
// same tab the member is using. The page renders at its real viewport and is
// scaled to fit the workspace (or shown at 100%); the size the page really
// has is a page property the user and every member share, edited right
// above the picture.
import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { sendBrowserInput, streamBrowserTab, type BrowserFrame, type BrowserInput } from '../../api/client';
import { useSetBrowserTabViewport, useTeamBrowser } from '../../api/queries';
import { apiErrorMessage } from '../../i18n/errors';
import { openExternal } from '../../platform/native-bridge';
import { Alert, EmptyState, Icons, IconButton, Select, TextInput } from '../../components/ui';
import { useElementSize } from './useElementSize';

const MOVE_INTERVAL_MS = 40;

function modifiers(e: { altKey: boolean; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }): number {
  return (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0);
}

/** How the picture is shown: scaled into the workspace, or at 100%. */
type Scale = 'fit' | 'full';

export default function BrowserTab({ teamId, panelTabId, onClose }: { teamId: string; panelTabId: string; onClose: () => void }) {
  const { t } = useTranslation();
  const state = useTeamBrowser(teamId);
  const setViewport = useSetBrowserTabViewport(teamId);
  const page = state.data?.tabs.find((item) => item.id === panelTabId);
  const gone = state.isSuccess && state.data !== undefined && !page;
  // A page without an address is nothing yet: no picture to paint (a blank
  // page reads as black), no stream to follow — an empty state and the
  // address bar wait for the first address.
  const blank = !page || !page.url || page.url === 'about:blank';

  const canvas = useRef<HTMLCanvasElement>(null);
  const addressInput = useRef<HTMLInputElement>(null);
  const [boxRef, box] = useElementSize<HTMLDivElement>();
  const frameSize = useRef({ width: 1280, height: 900 });
  const [error, setError] = useState<string | null>(null);
  const [address, setAddress] = useState('');
  const [editing, setEditing] = useState(false);
  const [hasFrame, setHasFrame] = useState(false);
  // The size the control row shows. frameSize is read on every pointer
  // event and stays a ref; this copy re-renders the row when a new
  // viewport arrives, which a ref alone never does.
  const [frame, setFrame] = useState({ width: 1280, height: 900 });
  const [scale, setScale] = useState<Scale>('fit');
  // The device preset the page's viewport is set to, when it is: the
  // dropdown shows it until a zoom mode or another size takes over.
  const [preset, setPreset] = useState<string | null>(null);
  const [viewportError, setViewportError] = useState<string | null>(null);
  // The sizes typed into the control row; they commit on enter or blur,
  // and follow the page again once it reports a new size.
  const [sizing, setSizing] = useState<{ width: string; height: string } | null>(null);

  // The first address of a blank page is typed, not found: the address bar
  // takes the focus.
  useEffect(() => {
    if (blank) addressInput.current?.focus();
  }, [blank]);
  // A size the page reports — ours committed, or another member's — is
  // what the inputs show again.
  useEffect(() => {
    setSizing(null);
  }, [frame.width, frame.height]);

  // Inputs go out one at a time and in order; a mouse move waiting behind
  // others is dropped, since the next one supersedes it.
  const queue = useRef<Promise<void>>(Promise.resolve());
  const pendingMoves = useRef(0);
  const send = useCallback(
    (input: BrowserInput) => {
      if (!panelTabId) return;
      // A blank page takes no picture and no pointer, but the address the
      // user just typed is exactly what moves it off blank.
      if (blank && input.kind !== 'nav') return;
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
    [teamId, panelTabId, blank, t],
  );

  // The pictures of this page. Only the visible tab mounts this component,
  // so a background tab never streams; a page that has no address has no
  // picture to stream either.
  useEffect(() => {
    if (!panelTabId || blank) return;
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
          const size = { width: frame.width || image.naturalWidth, height: frame.height || image.naturalHeight };
          frameSize.current = size;
          setFrame((current) => (current.width === size.width && current.height === size.height ? current : size));
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
  }, [teamId, panelTabId, blank, t]);

  // The address bar follows the page unless the user is typing in it; a
  // blank page shows none.
  useEffect(() => {
    if (!editing) setAddress(blank ? '' : (page?.url ?? ''));
  }, [page?.url, editing, blank]);

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
    if (!target || blank) return;
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
  }, [send, panelTabId, hasFrame, blank]);

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
  const natural = frame;
  // Fitting shrinks a page bigger than the box; a smaller one keeps its real size instead of scaling up into blur.
  const fitScale = box.width > 0 && box.height > 0 ? Math.min(box.width / natural.width, box.height / natural.height, 1) : 1;
  const shown = scale === 'fit' ? fitScale : 1;
  const shownWidth = Math.round(natural.width * shown);
  const shownHeight = Math.round(natural.height * shown);
  const percent = Math.round(shown * 100);

  const busy = page?.owner !== 'user' && (page?.busy ?? false);

  // One dropdown, one meaning at a time: the two zoom modes and the three
  // device presets. A preset sets the page's real viewport (for everyone)
  // and shows the whole device, so zoom goes back to fit; a zoom mode only
  // changes how the picture is drawn. Every step acts the same whichever
  // way the user walks through the list.
  const viewportValue = preset ?? scale;
  const applyViewport = (value: string) => {
    if (value === 'fit' || value === 'full') {
      setPreset(null);
      setScale(value);
      return;
    }
    setPreset(value);
    setScale('fit');
    setViewportError(null);
    setViewport.mutate(
      { tab: panelTabId, body: { preset: value as 'desktop' | 'tablet' | 'phone' } },
      { onError: (err) => setViewportError(apiErrorMessage(t, err)) },
    );
  };
  const commitSize = () => {
    const value = sizing;
    setSizing(null);
    setPreset(null);
    if (!value) return;
    const width = Number(value.width);
    const height = Number(value.height);
    if (!width || !height || width < 1 || height < 1 || width > 3840 || height > 2160) return;
    setViewportError(null);
    setViewport.mutate(
      { tab: panelTabId, body: { width, height } },
      { onError: (err) => setViewportError(apiErrorMessage(t, err)) },
    );
  };

  return (
    <div className="flex h-full min-h-0 flex-col gap-1.5 px-2 pb-2 pt-1.5">
      <div className="flex items-center gap-1">
        <IconButton label={t('browser.back')} icon={Icons.previous} size="sm" onClick={() => send({ kind: 'nav', to: 'back' })} disabled={blank} />
        <IconButton label={t('browser.forward')} icon={Icons.next} size="sm" onClick={() => send({ kind: 'nav', to: 'forward' })} disabled={blank} />
        <IconButton label={t('browser.reload')} icon={Icons.refresh} size="sm" onClick={() => send({ kind: 'nav', to: 'reload' })} disabled={blank} />
        <form
          className="min-w-0 flex-1"
          onSubmit={(e) => {
            e.preventDefault();
            if (address.trim()) send({ kind: 'nav', to: address.trim() });
            setEditing(false);
          }}
        >
          <TextInput
            ref={addressInput}
            size="sm"
            mono
            aria-label={t('browser.address')}
            placeholder={t('browser.addressPlaceholder')}
            value={address}
            onChange={(e) => setAddress(e.target.value)}
            onFocus={() => setEditing(true)}
            onBlur={() => setEditing(false)}
          />
        </form>
        {/* The page lives where the user's own tabs do: hand it to the
            system browser on their click, never on its own. */}
        <IconButton label={t('browser.openExternal')} icon={Icons.openExternal} size="sm" disabled={blank} onClick={() => page?.url && openExternal(page.url)} />
      </div>
      <div className="flex items-center gap-2 px-0.5" title={t('workspace.browser.viewportSizeTitle')}>
        {busy && <span role="img" className="h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" aria-label={t('workspace.browser.memberBusy')} />}
        <form
          className="flex shrink-0 items-center gap-1 font-mono text-2xs tabular-nums text-neutral-500 dark:text-neutral-400"
          onSubmit={(e) => {
            e.preventDefault();
            commitSize();
          }}
        >
          <input
            aria-label={t('workspace.browser.viewportWidth')}
            inputMode="numeric"
            value={sizing?.width ?? String(natural.width)}
            onChange={(e) => setSizing((current) => ({ width: e.target.value, height: current?.height ?? String(natural.height) }))}
            onBlur={commitSize}
            onKeyDown={(e) => e.key === 'Enter' && commitSize()}
            className="w-14 rounded-md border border-transparent bg-neutral-900/[0.04] px-1.5 py-0.5 text-right outline-none transition-colors focus:border-signal-500 dark:bg-white/[0.06]"
          />
          <span aria-hidden>×</span>
          <input
            aria-label={t('workspace.browser.viewportHeight')}
            inputMode="numeric"
            value={sizing?.height ?? String(natural.height)}
            onChange={(e) => setSizing((current) => ({ width: current?.width ?? String(natural.width), height: e.target.value }))}
            onBlur={commitSize}
            onKeyDown={(e) => e.key === 'Enter' && commitSize()}
            className="w-14 rounded-md border border-transparent bg-neutral-900/[0.04] px-1.5 py-0.5 text-right outline-none transition-colors focus:border-signal-500 dark:bg-white/[0.06]"
          />
        </form>
        <span className="shrink-0 font-mono text-2xs tabular-nums text-neutral-500 dark:text-neutral-400">
          {percent}%
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
        className={`relative min-h-0 flex-1 rounded-lg bg-neutral-200/60 ring-1 ring-inset ring-neutral-300 dark:bg-neutral-950 dark:ring-neutral-800 ${scale === 'full' ? 'overflow-auto' : 'overflow-hidden'}`}
      >
        {blank ? (
          <div className="flex h-full flex-col items-center justify-center gap-1.5">
            <Icons.webSearch size={22} className="text-neutral-400 dark:text-neutral-600" aria-hidden />
            <p className="text-sm font-medium text-neutral-600 dark:text-neutral-300">{t('browser.emptyTitle')}</p>
            <p className="text-xs text-neutral-500 dark:text-neutral-400">{t('browser.emptyHint')}</p>
          </div>
        ) : (
          <>
            <div className={scale === 'fit' ? 'flex h-full w-full items-center justify-center p-3' : 'inline-block p-3'}>
              {/* The page as a device: a framed picture sitting on the mat,
                  scaled to the workspace or shown one to one. */}
              <div className="overflow-hidden rounded-lg bg-white shadow-floating ring-1 ring-neutral-900/[0.08] dark:ring-white/[0.10]">
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
            </div>
            {!hasFrame && (
              <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-sm text-neutral-500 dark:text-neutral-400">
                {t('browser.loading')}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
