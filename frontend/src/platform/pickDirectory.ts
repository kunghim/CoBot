import { api } from '../api/client';
import { desktopBridge } from './native-bridge';

/**
 * pickDirectory shows the operating system's folder chooser and resolves to
 * the chosen absolute path, or '' when the user cancels. The desktop shell
 * opens it through its bridge; in the browser the local core opens it, since
 * a browser's own picker never reveals a real path. Errors (no chooser on
 * this machine, one already open) reject for the caller to report.
 */
export async function pickDirectory(): Promise<string> {
  const bridge = desktopBridge();
  if (bridge) return bridge.OpenDirectory();
  const result = await api.pickDirectory();
  return result.cancelled ? '' : result.path;
}
