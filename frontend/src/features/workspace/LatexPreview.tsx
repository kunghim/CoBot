import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { WorkspaceFile, WorkspaceLatexResult, WorkspaceScope } from '../../api/types';
import { Alert, Button } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { scopeKey } from './fileNavigation';
import { LatexToolbar } from './LatexToolbar';
import PDFPreview from './PDFPreview';
import SourcePreview from './SourcePreview';
import type { WorkspaceTab } from './state';

type View = NonNullable<WorkspaceTab['fileView']>;
export default function LatexPreview({ scope, file, view, onView }: { scope: WorkspaceScope; file: WorkspaceFile; view: View; onView: (view: View) => void }) {
  const { t } = useTranslation();
  const discovery = useQuery({ queryKey: ['workspace-latex', scopeKey(scope), file.path, file.version], queryFn: ({ signal }) => api.workspaceLatex(scope, file.path, signal), retry: false, staleTime: Infinity });
  const [chosen, setChosen] = useState<string>();
  const main = chosen ?? discovery.data?.suggested ?? '';
  const [pending, setPending] = useState(false);
  const [source, setSource] = useState(true);
  const [error, setError] = useState('');
  const [result, setResult] = useState<WorkspaceLatexResult>();
  const [preview, setPreview] = useState<{ data: Uint8Array; main: string; version: string }>();
  const running = useRef<AbortController>();
  useEffect(() => () => running.current?.abort(), []);
  useEffect(() => { if (view.line) setSource(true); }, [view.positionId]);
  const compile = async () => {
    running.current?.abort();
    const controller = new AbortController(); running.current = controller;
    setPending(true); setError(''); setResult(undefined);
    try {
      const compiled = await api.compileWorkspaceLatex(scope, main, controller.signal);
      if (controller.signal.aborted) return;
      setResult(compiled);
      if (compiled.success && compiled.pdf) {
        if (compiled.pdf.length > Math.ceil(32 * 1024 * 1024 / 3) * 4) throw new Error('Oversized PDF');
        const binary = atob(compiled.pdf);
        const data = Uint8Array.from(binary, (character) => character.charCodeAt(0));
        setChosen(compiled.main);
        setPreview({ data, main: compiled.main, version: file.version }); setSource(false);
      }
    } catch (err) { if (!controller.signal.aborted) setError(apiErrorMessage(t, err)); }
    finally { if (!controller.signal.aborted) setPending(false); }
  };
  return <div className="flex h-full min-h-0 flex-col">
    <LatexToolbar main={main} candidates={discovery.data?.candidates.map((candidate) => candidate.path) ?? []} available={!!discovery.data?.available} pending={pending} source={source} hasPDF={!!preview} onMain={setChosen} onCompile={() => void compile()} onCancel={() => { running.current?.abort(); setPending(false); }} onSource={setSource} />
    {discovery.isError ? <Alert tone="danger">{apiErrorMessage(t, discovery.error)} <Button size="sm" variant="ghost" onClick={() => void discovery.refetch()}>{t('workspace.files.retry')}</Button></Alert> : discovery.data && !discovery.data.available ? <Alert>{t('errors.error.workspace.latex_unavailable')}</Alert> : null}
    {discovery.data?.truncated && <p role="status" className="px-3 py-1 text-xs text-neutral-500">{t('workspace.files.latexTruncated')}</p>}
    {discovery.data && !main && <p role="status" className="px-3 py-1 text-xs text-neutral-500">{t('workspace.files.latexChoose')}</p>}
    {error && <Alert tone="danger">{error}</Alert>}
    {result && !result.success && <Alert tone="danger">{t(`errors.${result.errorKey ?? 'error.workspace.latex_failed'}`)} {preview && t('workspace.files.latexPrevious')}</Alert>}
    {preview && <p role="status" className="px-3 py-1 text-xs text-neutral-500">{t('workspace.files.latexCompiled', { main: preview.main })}{(preview.main !== main || preview.version !== file.version) && ` · ${t('workspace.files.latexStale')}`}</p>}
    {result?.log && <details className="max-h-48 shrink-0 overflow-auto border-b border-neutral-200 px-3 py-1 text-xs dark:border-neutral-800" open={!result.success}><summary className="cursor-pointer">{t('workspace.files.latexLog')}{result.logTruncated && ` · ${t('workspace.files.latexLogTruncated')}`}</summary><pre className="whitespace-pre-wrap break-words py-2 font-mono">{result.log}</pre></details>}
    <div className="min-h-0 flex-1">{source || !preview ? <SourcePreview scope={scope} file={file} view={view} onView={onView} /> : <PDFPreview scope={scope} file={file} data={preview.data} />}</div>
  </div>;
}
