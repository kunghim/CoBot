// NativeApprovalBody shows what a member's engine asks to do, by kind: the
// command it wants to run (without the shell wrapper), the files it wants
// to change with their diffs, or the extra access it asks for. Anything it
// cannot read stays visible as the stored text.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Icons } from '../../components/ui';
import DiffView from '../../components/DiffView';
import { displayCommand } from '../conversations/steps';
import { parseNativeRequest, type NativeRequest } from './nativeRequest';

export default function NativeApprovalBody({ summary, fallback }: { summary: string; fallback: string }) {
  const { t } = useTranslation();
  const request = parseNativeRequest(summary);
  if (!request) {
    return (
      <pre className="max-h-52 overflow-y-auto whitespace-pre-wrap break-all rounded-xl bg-neutral-900/[0.04] px-3 py-2 font-mono text-xs leading-5 text-neutral-800 dark:bg-black/30 dark:text-neutral-100">
        {fallback}
      </pre>
    );
  }
  return (
    <div className="space-y-1.5" data-testid="native-approval">
      {request.command !== undefined && (
        <>
          <p className="text-xs font-medium text-neutral-500 dark:text-neutral-400">{t('approvals.native.command')}</p>
          <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-all rounded-xl bg-neutral-900/[0.04] px-3 py-2 font-mono text-xs leading-5 text-neutral-800 dark:bg-black/30 dark:text-neutral-100" title={request.command}>
            {displayCommand(request.command)}
          </pre>
        </>
      )}
      {request.changes.length > 0 && (
        <>
          <p className="text-xs font-medium text-neutral-500 dark:text-neutral-400">{t('approvals.native.files', { count: request.changes.length })}</p>
          <ul className="space-y-1">
            {request.changes.map((change, index) => (
              <FileRow key={index} path={change.path} kind={change.kind} diff={change.diff} />
            ))}
          </ul>
        </>
      )}
      {request.changes.length === 0 && request.paths.length > 0 && (
        <>
          <p className="text-xs font-medium text-neutral-500 dark:text-neutral-400">{t('approvals.native.files', { count: request.paths.length })}</p>
          <ul className="space-y-0.5 font-mono text-xs">{request.paths.map((path) => <li key={path} className="break-all">{path}</li>)}</ul>
        </>
      )}
      {request.permissions && <PermissionList permissions={request.permissions} />}
      {request.network && (
        <p className="flex items-center gap-1 text-xs text-neutral-600 dark:text-neutral-300">
          <Icons.webSearch size={12} aria-hidden />
          {t('approvals.native.network')}
        </p>
      )}
      {request.cwd && <p className="break-all text-[11px] text-neutral-500 dark:text-neutral-400">{t('approvals.native.cwd', { cwd: request.cwd })}</p>}
    </div>
  );
}

function FileRow({ path, kind, diff }: { path: string; kind: string; diff: string }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <li className="text-xs">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        disabled={!diff}
        className="flex min-w-0 items-center gap-1.5 text-left text-neutral-800 enabled:hover:text-neutral-950 dark:text-neutral-200"
      >
        {diff ? open ? <Icons.expand size={12} aria-hidden /> : <Icons.next size={12} aria-hidden /> : <span className="w-3" />}
        <span className="shrink-0 rounded bg-neutral-900/[0.06] px-1 text-[10px] dark:bg-white/[0.08]">{t(`conversation.fileKind.${kind}`, { defaultValue: kind })}</span>
        <span className="min-w-0 break-all font-mono">{path}</span>
      </button>
      {open && diff && <div className="mt-1"><DiffView diff={diff} className="max-h-60" /></div>}
    </li>
  );
}

function PermissionList({ permissions }: { permissions: NonNullable<NativeRequest['permissions']> }) {
  const { t } = useTranslation();
  return (
    <div className="space-y-1 text-xs">
      <p className="font-medium text-neutral-500 dark:text-neutral-400">{t('approvals.native.permissions')}</p>
      <ul className="list-disc space-y-0.5 pl-5 text-neutral-800 dark:text-neutral-200">
        {permissions.network && <li>{t('approvals.native.permissionNetwork')}</li>}
        {permissions.read.length > 0 && <li className="break-all">{t('approvals.native.permissionRead', { paths: permissions.read.join('、') })}</li>}
        {permissions.write.length > 0 && <li className="break-all">{t('approvals.native.permissionWrite', { paths: permissions.write.join('、') })}</li>}
        {permissions.other && <li><pre className="whitespace-pre-wrap break-all font-mono text-[11px]">{permissions.other}</pre></li>}
      </ul>
    </div>
  );
}
