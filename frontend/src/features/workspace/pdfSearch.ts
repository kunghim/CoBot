export type PDFSearchMatch = { page: number; parts: Array<{ item: number; start: number; end: number }> };
type Item = { str: string; hasEOL?: boolean };
type SearchDocument = { numPages: number; getPage: (page: number) => Promise<{ getTextContent: () => Promise<{ items: unknown[] }> }> };

export function pageMatches(items: unknown[], query: string, page: number, maximum: number): PDFSearchMatch[] {
  const segments: Array<{ item: number; start: number; end: number }> = [];
  let text = '';
  let itemIndex = 0;
  for (const value of items) {
    if (!value || typeof value !== 'object' || !('str' in value) || typeof value.str !== 'string') continue;
    const item = value as Item;
    segments.push({ item: itemIndex++, start: text.length, end: text.length + item.str.length });
    text += item.str + (item.hasEOL ? ' ' : '');
    if (text.length > 2 * 1024 * 1024) throw new Error('PDF search limit');
  }
  const results: PDFSearchMatch[] = [];
  if (!query) return results;
  let offset = 0;
  while (results.length < maximum) {
    const start = text.indexOf(query, offset);
    if (start < 0) break;
    const end = start + query.length;
    let low = 0; let high = segments.length;
    while (low < high) { const middle = (low + high) >>> 1; if (segments[middle].end <= start) low = middle + 1; else high = middle; }
    const parts: PDFSearchMatch['parts'] = [];
    for (let index = low; index < segments.length && segments[index].start < end; index++) {
      const segment = segments[index];
      if (segment.end > segment.start) parts.push({ item: segment.item, start: Math.max(0, start - segment.start), end: Math.min(segment.end - segment.start, end - segment.start) });
    }
    if (parts.length) results.push({ page, parts });
    offset = start + Math.max(1, query.length);
  }
  return results;
}

export async function searchPDF(pdf: SearchDocument, query: string, signal: AbortSignal): Promise<{ matches: PDFSearchMatch[]; limited: boolean }> {
  const matches: PDFSearchMatch[] = [];
  if (!query || query.length > 256) return { matches, limited: false };
  for (let page = 1; page <= pdf.numPages; page++) {
    signal.throwIfAborted();
    const document = await pdf.getPage(page);
    signal.throwIfAborted();
    const content = await document.getTextContent();
    signal.throwIfAborted();
    matches.push(...pageMatches(content.items, query, page, 201 - matches.length));
    if (matches.length > 200) return { matches: matches.slice(0, 200), limited: true };
  }
  return { matches, limited: false };
}
