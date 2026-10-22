// The desktop window as the page sees it. The Wails shell hides the system
// title bar on Windows (frameless) and macOS (hidden inset, traffic lights
// kept), so the page draws its own: it needs the platform and the window
// controls of the Wails runtime, which the shell injects as window.runtime.
// In a browser there is no runtime and no title bar.
import { useEffect, useState } from 'react';

interface WailsRuntime {
  Environment(): Promise<{ platform: string }>;
  WindowMinimise(): void;
  WindowToggleMaximise(): void;
  WindowIsMaximised(): Promise<boolean>;
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
  minimize: () => void;
  toggleMaximize: () => void;
  close: () => void;
}

/** The desktop window, or null in a browser (and on Linux, which keeps the system bar). */
export function useDesktopWindow(): DesktopWindow | null {
  const [platform, setPlatform] = useState<TitleBarPlatform | null>(null);
  const [maximized, setMaximized] = useState(false);
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
    const refresh = () => void rt.WindowIsMaximised().then((value) => !cancelled && setMaximized(value));
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
    minimize: () => rt.WindowMinimise(),
    toggleMaximize: () => rt.WindowToggleMaximise(),
    close: () => rt.Quit(),
  };
}
