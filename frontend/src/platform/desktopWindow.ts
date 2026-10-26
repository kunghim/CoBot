// The desktop window as the page sees it. The Wails shell hides the system
// title bar on Windows (frameless) and macOS (hidden inset, traffic lights
// kept), so the page draws its own header rows: it needs the platform and
// the window controls of the Wails runtime, which the shell injects as
// window.runtime. In a browser there is no runtime and no header rows.
import { createContext, useContext, useEffect, useState } from 'react';

interface WailsRuntime {
  Environment(): Promise<{ platform: string }>;
  WindowMinimise(): void;
  WindowToggleMaximise(): void;
  WindowIsMaximised(): Promise<boolean>;
  WindowIsFullscreen(): Promise<boolean>;
  WindowSetTitle?(title: string): void;
  Quit(): void;
}

function runtime(): WailsRuntime | null {
  const w = window as unknown as { runtime?: Partial<WailsRuntime>; go?: unknown };
  // The runtime alone is not proof: only the CoBot shell also binds go.*.
  if (!w.go || typeof w.runtime?.Environment !== 'function') return null;
  return w.runtime as WailsRuntime;
}

/** Platforms whose shell hides the system title bar. */
export type TitleBarPlatform = 'windows' | 'mac';

export interface DesktopWindow {
  platform: TitleBarPlatform;
  maximized: boolean;
  fullscreen: boolean;
  minimize: () => void;
  toggleMaximize: () => void;
  close: () => void;
  /** Names the window: macOS draws it beside the traffic lights when the
   * fullscreen hover reveals the titlebar. Empty string clears it. */
  setNativeTitle: (title: string) => void;
}

/** The app shell provides the window once, high above the layouts; column
 * headers and the corner reservation read it through this context. */
export const DesktopWindowContext = createContext<DesktopWindow | null>(null);

/** The window frame, or null in a plain browser tab. */
export function useWindowFrame(): DesktopWindow | null {
  return useContext(DesktopWindowContext);
}

/** The desktop window, or null in a browser (and on Linux, which keeps the system bar). */
export function useDesktopWindow(): DesktopWindow | null {
  const [platform, setPlatform] = useState<TitleBarPlatform | null>(null);
  const [maximized, setMaximized] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  useEffect(() => {
    const rt = runtime();
    if (!rt) return;
    let cancelled = false;
    void rt.Environment().then((env) => {
      if (cancelled) return;
      if (env.platform === 'windows') setPlatform('windows');
      else if (env.platform === 'darwin') setPlatform('mac');
    });
    // Maximizing resizes the page; that is when to read the state again.
    // An injected runtime that predates WindowIsFullscreen must not also
    // freeze the maximize state.
    const readFullscreen = () => (typeof rt.WindowIsFullscreen === 'function' ? rt.WindowIsFullscreen() : Promise.resolve(false));
    const refresh = () => void Promise.all([rt.WindowIsMaximised(), readFullscreen()])
      .then(([maximized, isFullscreen]) => {
        if (!cancelled) {
          setMaximized(maximized);
          setFullscreen(isFullscreen);
        }
      })
      .catch(() => undefined);
    refresh();
    window.addEventListener('resize', refresh);
    return () => {
      cancelled = true;
      window.removeEventListener('resize', refresh);
    };
  }, []);
  const rt = runtime();
  if (!platform || !rt) return null;
  return {
    platform,
    maximized,
    fullscreen,
    minimize: () => rt.WindowMinimise(),
    toggleMaximize: () => rt.WindowToggleMaximise(),
    close: () => rt.Quit(),
    setNativeTitle: (title: string) => rt.WindowSetTitle?.(title),
  };
}
