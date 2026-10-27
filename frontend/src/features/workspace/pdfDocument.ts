import { GlobalWorkerOptions, PDFDataRangeTransport, getDocument, version } from 'pdfjs-dist/legacy/build/pdf.mjs';
import workerURL from 'pdfjs-dist/legacy/build/pdf.worker.min.mjs?url';
import { api } from '../../api/client';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';

GlobalWorkerOptions.workerSrc = workerURL;
const CHUNK = 64 * 1024;

class WorkspacePDFTransport extends PDFDataRangeTransport {
  failure: (error: unknown) => void = () => {};
  constructor(private scope: WorkspaceScope, private file: WorkspaceFile, initial: Uint8Array, private controller: AbortController) {
    super(file.size, initial, true);
  }
  requestDataRange(begin: number, end: number): void {
    api.workspaceBytes(this.scope, this.file.path, this.controller.signal, `bytes=${begin}-${end - 1}`, this.file.version)
      .then((blob) => blob.arrayBuffer())
      .then((bytes) => { if (!this.controller.signal.aborted) this.onDataRange(begin, new Uint8Array(bytes)); })
      .catch((error) => { if (!this.controller.signal.aborted) this.failure(error); });
  }
  abort(): void { this.controller.abort(); }
}

export async function loadWorkspacePDF(scope: WorkspaceScope, file: WorkspaceFile, controller: AbortController, data?: Uint8Array) {
  if (data) return { task: getDocument({ ...pdfOptions(), data: data.slice() }), range: undefined };
  const initial = await api.workspaceBytes(scope, file.path, controller.signal, `bytes=0-${Math.min(file.size, CHUNK) - 1}`, file.version);
  controller.signal.throwIfAborted();
  const range = new WorkspacePDFTransport(scope, file, new Uint8Array(await initial.arrayBuffer()), controller);
  const task = getDocument({ ...pdfOptions(), range, rangeChunkSize: CHUNK, disableStream: true, disableAutoFetch: true });
  return { task, range };
}

function pdfOptions() {
  const base = new URL(`${import.meta.env.BASE_URL}assets/pdfjs/${version}/`, window.location.href).href;
  return { cMapUrl: base + 'cmaps/', standardFontDataUrl: base + 'standard_fonts/', wasmUrl: base + 'wasm/', iccUrl: base + 'iccs/', useWorkerFetch: true, useSystemFonts: false, enableXfa: false, maxImageSize: 16_000_000, canvasMaxAreaInBytes: 64 * 1024 * 1024 };
}
