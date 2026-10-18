// Native bridge surface of the Wails desktop shell. The generated wailsjs
// directory is gitignored and may be absent on a clean checkout, so the
// bridge is declared structurally here instead of statically importing it.
// Browser detection falls back to the pure-HTTP startup flow.

interface DesktopBridge {
  /** Waits for the core to be ready and returns a one-time bootstrap secret
   * plus the loopback endpoint. */
  Bootstrap(): Promise<{ secret: string; endpoint: string }>;
  /** Opens the native directory picker; returns the chosen path or empty. */
  OpenDirectory(): Promise<string>;
  /** Asks the host to quit the application. */
  Quit(): Promise<void>;
}

interface BridgeWindow {
  go?: { desktop?: { Bridge?: DesktopBridge } };
}

export function desktopBridge(): DesktopBridge | null {
  const w = window as unknown as BridgeWindow;
  return w.go?.desktop?.Bridge ?? null;
}
