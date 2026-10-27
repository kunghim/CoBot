const MAX_EXPANDED = 64 * 1024 * 1024;

/** Bound expansion before handing the same archive to the workbook parser. */
export async function validateWorkbookArchive(buffer: ArrayBuffer, mainPart = 'xl/workbook.xml'): Promise<void> {
  const bytes = new Uint8Array(buffer);
  const view = new DataView(buffer);
  const invalid = () => { throw new Error('Invalid workbook'); };
  const limit = () => { throw new Error('Workbook limit'); };
  if (bytes.length < 22 || bytes.length > 10 * 1024 * 1024) return invalid();
  let end = bytes.length - 22;
  while (end >= Math.max(0, bytes.length - 65557) && view.getUint32(end, true) !== 0x06054b50) end--;
  if (end < 0 || end < bytes.length - 65557 || end + 22 + view.getUint16(end + 20, true) !== bytes.length) return invalid();
  if (view.getUint16(end + 4, true) || view.getUint16(end + 6, true)) return invalid();
  const count = view.getUint16(end + 10, true);
  if (count > 4096) return limit();
  if (count !== view.getUint16(end + 8, true)) return invalid();
  let cursor = view.getUint32(end + 16, true);
  const directoryEnd = cursor + view.getUint32(end + 12, true);
  if (directoryEnd !== end) return invalid();
  let expanded = 0;
  const names = new Set<string>();
  for (let index = 0; index < count; index++) {
    if (cursor + 46 > end || view.getUint32(cursor, true) !== 0x02014b50) return invalid();
    const flags = view.getUint16(cursor + 8, true);
    const method = view.getUint16(cursor + 10, true);
    const packed = view.getUint32(cursor + 20, true);
    const size = view.getUint32(cursor + 24, true);
    const nameLength = view.getUint16(cursor + 28, true);
    const next = cursor + 46 + nameLength + view.getUint16(cursor + 30, true) + view.getUint16(cursor + 32, true);
    if (next > end || flags & 1 || ![0, 8].includes(method)) return invalid();
    expanded += size;
    if (expanded > MAX_EXPANDED) return limit();
    const name = new TextDecoder('utf-8', { fatal: true }).decode(bytes.subarray(cursor + 46, cursor + 46 + nameLength));
    if (names.has(name) || name.includes('\\') || name.split('/').includes('..') || name.startsWith('/')) return invalid();
    names.add(name);
    const local = view.getUint32(cursor + 42, true);
    if (local + 30 > directoryEnd || view.getUint32(local, true) !== 0x04034b50 || view.getUint16(local + 8, true) !== method || view.getUint16(local + 6, true) !== flags) return invalid();
    const localNameLength = view.getUint16(local + 26, true);
    const start = local + 30 + localNameLength + view.getUint16(local + 28, true);
    if (start + packed > view.getUint32(end + 16, true) || localNameLength !== nameLength || !bytes.subarray(local + 30, local + 30 + nameLength).every((byte, offset) => byte === bytes[cursor + 46 + offset])) return invalid();
    if (method === 0) { if (packed !== size) return invalid(); }
    else {
      // Checking actual output also rejects an archive that lies about its
      // uncompressed size in the central directory.
      const stream = new Blob([bytes.slice(start, start + packed)]).stream().pipeThrough(new DecompressionStream('deflate-raw'));
      const reader = stream.getReader();
      let actual = 0;
      try {
        while (true) { const chunk = await reader.read(); if (chunk.done) break; actual += chunk.value.byteLength; if (actual > size || actual > MAX_EXPANDED) { await reader.cancel(); return limit(); } }
      } finally { reader.releaseLock(); }
      if (actual !== size) return invalid();
    }
    cursor = next;
  }
  if (cursor !== directoryEnd || !names.has('[Content_Types].xml') || !names.has(mainPart)) return invalid();
}
