export type AppMode = 'chat' | 'work';
export function readLocal(key: string, fallback = ''): string {
  try { return localStorage.getItem(key) ?? fallback; } catch { return fallback; }
}
export function writeLocal(key: string, value: string): void {
  try { localStorage.setItem(key, value); } catch { /* Browsing without storage still works. */ }
}
export function savedMode(): AppMode | null {
  const value = readLocal('cobot.mode');
  return value === 'chat' || value === 'work' ? value : null;
}
export function lastChatPath(): string {
  const id = readLocal('cobot.chat.selected');
  return /^[a-zA-Z0-9-]+$/.test(id) && id !== 'new' ? `/chat/${id}` : '/chat/new';
}
