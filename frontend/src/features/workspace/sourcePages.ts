export const SOURCE_CHUNK_BYTES = 64 * 1024;
export const SOURCE_PAGE_LINES = 500;

export function sourcePage(bytes: Uint8Array, atEnd: boolean): { text: string; consumed: number; newlines: number } {
  const decoder = new TextDecoder('utf-8', { fatal: true });
  const decoded = decoder.decode(bytes, { stream: !atEnd });
  const text = !atEnd && decoded.endsWith('\r') ? decoded.slice(0, -1) : decoded;
  const breaks = [...text.matchAll(/\r\n|\r|\n/g)];
  const boundary = breaks.length >= SOURCE_PAGE_LINES ? breaks[SOURCE_PAGE_LINES - 1] : !atEnd && breaks.length ? breaks[breaks.length - 1] : undefined;
  const shown = boundary ? text.slice(0, boundary.index! + boundary[0].length) : text;
  return { text: shown, consumed: new TextEncoder().encode(shown).length, newlines: [...shown.matchAll(/\r\n|\r|\n/g)].length };
}
