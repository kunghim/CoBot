import { renderAsync } from 'docx-preview';
import { validateWorkbookArchive } from './workbookArchive';
const root = document.getElementById('document')!;
let loaded = false;
let zoom: number | null = null;
function scale() {
  const page = root.querySelector<HTMLElement>('section.docx');
  root.style.zoom = String(zoom ?? Math.min(1, (innerWidth - 24) / ((page?.offsetWidth ?? 794) + 32)));
}
addEventListener('resize', scale);
// No document link may navigate this frame, including links to local files.
addEventListener('click', (event) => { if ((event.target as Element)?.closest('a')) event.preventDefault(); }, true);
addEventListener('submit', (event) => event.preventDefault(), true);
addEventListener('message', async (event) => {
  if (event.source !== parent) return;
  if (event.data?.type === 'zoom') { zoom = typeof event.data.value === 'number' ? Math.max(0.25, Math.min(2, event.data.value)) : null; scale(); return; }
  if (loaded || event.data?.type !== 'document' || !(event.data.buffer instanceof ArrayBuffer)) return;
  loaded = true;
  try {
    await validateWorkbookArchive(event.data.buffer, 'word/document.xml');
    await renderAsync(event.data.buffer, root, undefined, { useBase64URL: true, renderAltChunks: false, renderComments: false, renderChanges: false, ignoreLastRenderedPageBreak: false, experimental: false });
    const pages = root.querySelectorAll('section.docx').length;
    if (pages > 200 || root.querySelectorAll('*').length > 100000) throw new Error('Document limit');
    scale();
    parent.postMessage({ type: 'docx-ready', pages }, '*');
  } catch { root.replaceChildren(); parent.postMessage({ type: 'docx-error' }, '*'); }
});
