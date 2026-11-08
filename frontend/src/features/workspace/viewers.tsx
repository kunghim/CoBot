import { lazy, type ComponentType } from 'react';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';
import ImagePreview from './ImagePreview';
import MarkdownPreview from './MarkdownPreview';
import SourcePreview from './SourcePreview';
import type { WorkspaceTab } from './state';

const LatexPreview = lazy(() => import('./LatexPreview'));
const PPTXPreview = lazy(() => import('./PPTXPreview'));
const DOCXPreview = lazy(() => import('./DOCXPreview'));
const WorkbookPreview = lazy(() => import('./WorkbookPreview'));
const TablePreview = lazy(() => import('./TablePreview'));
const MediaPreview = lazy(() => import('./MediaPreview'));
const NotebookPreview = lazy(() => import('./NotebookPreview'));
const HTMLPreview = lazy(() => import('./HTMLPreview'));
const PDFPreview = lazy(() => import('./PDFPreview'));

type View = NonNullable<WorkspaceTab['fileView']>;
export type ViewerProps = { scope: WorkspaceScope; file: WorkspaceFile; view: View; onView: (view: View) => void };

export type FileViewer = {
  id: string;
  component: ComponentType<ViewerProps>;
  // Starts over for each version of the file instead of updating in place.
  perVersion?: boolean;
  // Absent means it always applies.
  applies?: (file: WorkspaceFile, source: boolean) => boolean;
};

const asPreview = (_: WorkspaceFile, source: boolean) => !source;
const delimited = (file: WorkspaceFile) => /\.(csv|tsv)$/i.test(file.name);

const source: FileViewer = { id: 'source', component: SourcePreview };
const image: FileViewer = { id: 'image', component: ImagePreview };
const media: FileViewer = { id: 'media', component: MediaPreview };

// For each kind, the viewers in the order they are tried. Keyed by the
// interface's own list, so a kind added there does not compile until it is here.
// A kind with none shows that the file cannot be previewed.
export const viewers: Record<WorkspaceFile['kind'], FileViewer[]> = {
  directory: [],
  text: [source],
  markdown: [{ id: 'markdown', component: MarkdownPreview, applies: asPreview }, source],
  html: [{ id: 'html', component: HTMLPreview, applies: asPreview }, source],
  svg: [{ ...image, applies: asPreview }, source],
  image: [image],
  pdf: [{ id: 'pdf', component: PDFPreview }],
  audio: [media],
  video: [media],
  document: [{ id: 'document', component: DOCXPreview, perVersion: true }],
  spreadsheet: [
    { id: 'workbook', component: WorkbookPreview, applies: (file) => !delimited(file) },
    { id: 'table', component: TablePreview, applies: asPreview },
    source,
  ],
  presentation: [{ id: 'presentation', component: PPTXPreview, perVersion: true }],
  notebook: [{ id: 'notebook', component: NotebookPreview, applies: asPreview }, source],
  latex: [{ id: 'latex', component: LatexPreview }],
  binary: [],
  blocked: [],
};

export function pickViewer(file: WorkspaceFile, showSource: boolean): FileViewer | undefined {
  return viewers[file.kind].find((viewer) => !viewer.applies || viewer.applies(file, showSource));
}

// The button shows when it would change what is on screen.
export function showsSourceButton(file: WorkspaceFile): boolean {
  return pickViewer(file, false)?.id !== pickViewer(file, true)?.id;
}
