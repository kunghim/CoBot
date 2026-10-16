import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { UserPlus, X } from 'lucide-react';
import { api } from '../../api/client';
import { useRoles } from '../../api/queries';
import type { TeamWithBots } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import { useDialogFocus } from '../../components/useDialogFocus';

// TeamTools adds a member to an active team. A team keeps its folder for
// life, so there is no directory move or marker repair here: CoBot follows
// the folder by itself (see CheckTeamDirectories).
export default function TeamTools({ item }: { item: TeamWithBots }) {
  const { t } = useTranslation();
  const { team } = item;
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [roleId, setRoleId] = useState('');
  const [notice, setNotice] = useState('');
  const roles = useRoles(open);
  const mutate = useMutation({
    mutationFn: async () => {
      await api.addBot(team.id, roleId);
    },
    onSuccess: () => { setNotice(t('team.memberAdded')); setOpen(false); void qc.invalidateQueries(); },
  });
  const dialogRef = useDialogFocus(open, () => setOpen(false), mutate.isPending);
  if (team.status !== 'active') return null;
  const show = () => { setOpen(true); setRoleId(''); setNotice(''); mutate.reset(); };
  return <div className="mt-1 space-y-1 pl-6">
    <div className="flex gap-1"><button type="button" onClick={show} aria-label={t('team.tools.add')} title={t('team.tools.add')} className="rounded p-1.5 text-neutral-400 hover:bg-neutral-200 hover:text-neutral-800 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"><UserPlus size={14} aria-hidden="true" /></button></div>
    {notice && <p role="status" className="break-words text-xs text-neutral-500">{notice}</p>}
    {open && <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4">
      <form ref={dialogRef as React.RefObject<HTMLFormElement>} tabIndex={-1} role="dialog" aria-modal="true" aria-labelledby={`team-tool-${team.id}`} className="w-full max-w-md space-y-3 rounded-xl border border-neutral-200 bg-white p-4 dark:border-neutral-700 dark:bg-neutral-900" onSubmit={(event) => { event.preventDefault(); mutate.mutate(); }}>
        <header className="flex items-center gap-2"><h2 id={`team-tool-${team.id}`} className="flex-1 text-sm font-semibold">{t('team.tools.add')} · {team.name}</h2><button type="button" aria-label={t('team.closeTool')} onClick={() => setOpen(false)} disabled={mutate.isPending} className="rounded p-1"><X size={16} /></button></header>
        <label className="block text-sm">{t('team.roleLabel')}<select value={roleId} onChange={(event) => setRoleId(event.target.value)} className="mt-1 w-full rounded border border-neutral-300 bg-transparent px-2 py-2 dark:border-neutral-700"><option value="">{t('team.chooseRole')}</option>{roles.data?.items.filter((role) => role.id !== 'leader').map((role) => <option value={role.id} key={role.id}>{t(role.nameKey, { defaultValue: role.name })}</option>)}</select></label>
        {roles.isError && <p role="alert" className="text-xs text-red-600">{t('team.loadFailed')}</p>}
        {mutate.isError && <p role="alert" className="text-xs text-red-600">{apiErrorMessage(t, mutate.error)}</p>}
        <button type="submit" disabled={mutate.isPending || !roleId} className="rounded bg-neutral-900 px-3 py-2 text-sm text-white disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900">{t(mutate.isPending ? 'status.loading' : 'team.tools.add')}</button>
      </form>
    </div>}
  </div>;
}
