import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { FileCheck, FolderInput, UserPlus, X } from 'lucide-react';
import { api } from '../../api/client';
import { useRoles } from '../../api/queries';
import type { TeamWithBots } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import { desktopBridge } from '../../platform/native-bridge';

type Action = 'add' | 'rebind' | 'rebuild';

export default function TeamTools({ item }: { item: TeamWithBots }) {
  const { t } = useTranslation();
  const { team } = item;
  const qc = useQueryClient();
  const [action, setAction] = useState<Action | null>(null);
  const [roleId, setRoleId] = useState('');
  const [directory, setDirectory] = useState('');
  const [adoptMarker, setAdoptMarker] = useState(false);
  const [notice, setNotice] = useState('');
  const roles = useRoles(action === 'add');
  const mutate = useMutation({
    mutationFn: async (chosen: Action) => {
      if (chosen === 'add') { await api.addBot(team.id, roleId); return 'team.memberAdded'; }
      if (chosen === 'rebind') { await api.rebindDirectory(team.id, { directory: directory.trim(), adoptMarker, expectedRevision: team.revision }); return 'team.directoryRebound'; }
      const result = await api.rebuildMarker(team.id, team.revision);
      if (!result.rebuilt) return 'team.rebuildUnconfirmed';
      return result.resourcesLost ? 'team.rebuiltResourcesLost' : 'team.markerRebuilt';
    },
    onSuccess: (messageKey) => { setNotice(t(messageKey)); setAction(null); void qc.invalidateQueries(); },
  });
  const open = (chosen: Action) => { setAction(chosen); setRoleId(''); setDirectory(team.directory); setAdoptMarker(false); setNotice(''); mutate.reset(); };
  const available: Action[] = team.status === 'active' ? ['add', 'rebind', 'rebuild'] : team.status === 'directory_missing' || team.status === 'error' ? ['rebind', 'rebuild'] : [];
  const icon = { add: UserPlus, rebind: FolderInput, rebuild: FileCheck };
  return <div className="mt-1 space-y-1 pl-6">
    <div className="flex gap-1">{available.map((id) => { const Icon = icon[id]; return <button key={id} type="button" onClick={() => open(id)} aria-label={t(`team.tools.${id}`)} title={t(`team.tools.${id}`)} className="rounded p-1.5 text-neutral-400 hover:bg-neutral-200 hover:text-neutral-800 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"><Icon size={14} aria-hidden="true" /></button>; })}</div>
    {notice && <p role="status" className="break-words text-xs text-neutral-500">{notice}</p>}
    {action && <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" onKeyDown={(event) => { if (event.key === 'Escape' && !mutate.isPending) setAction(null); }}>
      <form role="dialog" aria-modal="true" aria-labelledby={`team-tool-${team.id}`} className="w-full max-w-md space-y-3 rounded-xl border border-neutral-200 bg-white p-4 dark:border-neutral-700 dark:bg-neutral-900" onSubmit={(event) => { event.preventDefault(); mutate.mutate(action); }}>
        <header className="flex items-center gap-2"><h2 id={`team-tool-${team.id}`} className="flex-1 text-sm font-semibold">{t(`team.tools.${action}`)} · {team.name}</h2><button type="button" aria-label={t('team.closeTool')} onClick={() => setAction(null)} disabled={mutate.isPending} className="rounded p-1"><X size={16} /></button></header>
        {action === 'add' && <label className="block text-sm">{t('team.roleLabel')}<select autoFocus value={roleId} onChange={(event) => setRoleId(event.target.value)} className="mt-1 w-full rounded border border-neutral-300 bg-transparent px-2 py-2 dark:border-neutral-700"><option value="">{t('team.chooseRole')}</option>{roles.data?.items.filter((role) => role.id !== 'leader').map((role) => <option value={role.id} key={role.id}>{t(role.nameKey, { defaultValue: role.name })}</option>)}</select></label>}
        {action === 'add' && roles.isError && <p role="alert" className="text-xs text-red-600">{t('team.loadFailed')}</p>}
        {action === 'rebind' && <>
          <label className="block text-sm">{t('team.directoryLabel')}<input autoFocus value={directory} onChange={(event) => setDirectory(event.target.value)} className="mt-1 w-full rounded border border-neutral-300 bg-transparent px-2 py-2 dark:border-neutral-700" /></label>
          {desktopBridge() && <button type="button" className="text-xs underline" onClick={() => { void desktopBridge()!.OpenDirectory().then((path) => { if (path) setDirectory(path); }); }}>{t('team.browse')}</button>}
          <label className="flex items-start gap-2 text-xs"><input type="checkbox" checked={adoptMarker} onChange={(event) => setAdoptMarker(event.target.checked)} />{t('team.adoptMarker')}</label>
        </>}
        {action === 'rebuild' && <p className="text-sm text-neutral-500">{t('team.rebuildExplanation')}</p>}
        {mutate.isError && <p role="alert" className="text-xs text-red-600">{apiErrorMessage(t, mutate.error)}</p>}
        <button type="submit" disabled={mutate.isPending || (action === 'add' && !roleId) || (action === 'rebind' && !directory.trim())} className="rounded bg-neutral-900 px-3 py-2 text-sm text-white disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900">{t(mutate.isPending ? 'status.loading' : `team.tools.${action}`)}</button>
      </form>
    </div>}
  </div>;
}
