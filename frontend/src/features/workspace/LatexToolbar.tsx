import { useTranslation } from 'react-i18next';
import { Button, Select, TextInput } from '../../components/ui';

export function LatexToolbar({ main, candidates, available, pending, source, hasPDF, onMain, onCompile, onCancel, onSource }: {
  main: string; candidates: string[]; available: boolean; pending: boolean; source: boolean; hasPDF: boolean;
  onMain: (value: string) => void; onCompile: () => void; onCancel: () => void; onSource: (value: boolean) => void;
}) {
  const { t } = useTranslation();
  return <div className="flex flex-wrap items-center gap-2 border-b border-neutral-200 px-3 py-2 text-xs dark:border-neutral-800">
    {candidates.length > 1 && <Select size="sm" label={t('workspace.files.latexCandidates')} options={candidates.map((path) => ({ value: path, label: path }))} value={main} onChange={onMain} disabled={pending} placeholder={t('workspace.files.latexChoose')} />}
    <label className="flex min-w-40 flex-1 items-center gap-2"><span className="shrink-0">{t('workspace.files.latexMain')}</span><TextInput size="sm" aria-label={t('workspace.files.latexMain')} placeholder="main.tex" value={main} onChange={(event) => onMain(event.target.value)} disabled={pending} className="min-w-0 flex-1 font-mono" /></label>
    {pending ? <Button size="sm" onClick={onCancel}>{t('workspace.files.latexCancel')}</Button> : <Button size="sm" disabled={!available || !main.trim()} onClick={onCompile}>{t('workspace.files.latexCompile')}</Button>}
    <Button size="sm" variant={source ? 'secondary' : 'ghost'} onClick={() => onSource(true)}>{t('workspace.files.source')}</Button>
    <Button size="sm" variant={!source ? 'secondary' : 'ghost'} disabled={!hasPDF} onClick={() => onSource(false)}>PDF</Button>
  </div>;
}
