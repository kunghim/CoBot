export type SourceMatch = { line: number; column: number };

// Keep only the overlap needed to recognize a match across transport chunks.
export async function searchSource(chunks: AsyncIterable<string>, query: string, limit = 200): Promise<{ matches: SourceMatch[]; limited: boolean }> {
  const matches: SourceMatch[] = [];
  if (!query || /[\r\n]/.test(query)) return { matches, limited: false };
  let tail = '';
  let line = 1;
  let column = 1;
  const advance = (text: string) => {
    const parts = text.split(/\r\n|\r|\n/);
    if (parts.length > 1) { line += parts.length - 1; column = parts[parts.length - 1].length + 1; }
    else column += text.length;
  };
  for await (const chunk of chunks) {
    const text = tail + chunk;
    // A CRLF pair and a literal query must stay intact at the boundary.
    let boundary = Math.max(0, text.length - query.length);
    if (text[boundary - 1] === '\r' && text[boundary] === '\n') boundary--;
    let scanned = 0;
    for (let index = text.indexOf(query); index >= 0 && index < boundary; index = text.indexOf(query, index + 1)) {
      advance(text.slice(scanned, index)); scanned = index;
      matches.push({ line, column });
      if (matches.length >= limit) return { matches, limited: true };
    }
    advance(text.slice(scanned, boundary));
    tail = text.slice(boundary);
  }
  let scanned = 0;
  for (let index = tail.indexOf(query); index >= 0; index = tail.indexOf(query, index + 1)) {
    advance(tail.slice(scanned, index)); scanned = index;
    matches.push({ line, column });
    if (matches.length >= limit) return { matches, limited: true };
  }
  return { matches, limited: false };
}
