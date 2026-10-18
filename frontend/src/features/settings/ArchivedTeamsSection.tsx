import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { useDeleteTeam, useRestoreTeam, useTeams } from '../../api/queries';
import type { TeamWithBots } from '../../api/types';
import ConfirmDialog from '../../components/ConfirmDialog';
import { apiErrorMessage } from '../../i18n/errors';

type SortKey = 'archivedAt' | 'createdAt' | 'name';

const buttonClass =
  'rounded-md border border-neutral-300 px-2 py-1 text-xs text-neutral-600 hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800';

/**
 * ArchivedTeamsSection lists the teams set aside by archiving. Archiving
 * only succeeds for an idle team, so any of them can be restored to the
 * sidebar or deleted outright, one at a time or several at once. A team id
 * in ?team= (from the new-team dialog) is highlighted.
 */
export default function ArchivedTeamsSection() {
  const { t, i18n } = useTranslation();
  const teams = useTeams(true);
  const restore = useRestoreTeam();
  const remove = useDeleteTeam();
  const [params] = useSearchParams();
  const focusId = params.get('team') ?? '';
  const [search, setSearch] = useState('');
  const [sort, setSort] = useState<SortKey>('archivedAt');
  const [selected, setSelected] = useState<ReadonlySet<string>>(() => new Set());
  const [pendingDelete, setPendingDelete] = useState<string[] | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [notice, setNotice] = useState('');
  const [errorText, setErrorText] = useState('');
  const focusRef = useRef<HTMLLIElement | null>(null);

  // The archived list must be complete to search and sort it.
  useEffect(() => {
    if (teams.hasNextPage && !teams.isFetchingNextPage) void teams.fetchNextPage();
  }, [teams, teams.hasNextPage, teams.isFetchingNextPage]);

  const archived = useMemo(
    () => (teams.data?.items ?? []).filter((item) => item.team.status === 'archived'),
    [teams.data],
  );
  const visible = useMemo(() => {
    const needle = search.trim().toLowerCase();
    const matched = needle
      ? archived.filter(
          (item) => item.team.name.toLowerCase().includes(needle) || item.team.directory.toLowerCase().includes(needle),
        )
      : archived;
    const by = (item: TeamWithBots) =>
      sort === 'name' ? item.team.name : sort === 'createdAt' ? item.team.createdAt : (item.team.archivedAt ?? '');
    return [...matched].sort((a, b) =>
      sort === 'name' ? by(a).localeCompare(by(b), i18n.language) : by(b).localeCompare(by(a)),
    );
  }, [archived, search, sort, i18n.language]);

  // Selection never outlives the teams it names.
  useEffect(() => {
    setSelected((prev) => {
      const live = new Set(archived.map((item) => item.team.id));
      const next = new Set([...prev].filter((id) => live.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [archived]);

  useEffect(() => {
    if (focusId && focusRef.current) focusRef.current.scrollIntoView({ block: 'center' });
  }, [focusId, visible]);

  const allVisibleSelected = visible.length > 0 && visible.every((item) => selected.has(item.team.id));
  const toggle = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const toggleAll = () =>
    setSelected((prev) => {
      const next = new Set(prev);
      for (const item of visible) {
        if (allVisibleSelected) next.delete(item.team.id);
        else next.add(item.team.id);
      }
      return next;
    });

  const date = (value?: string) => (value ? new Date(value).toLocaleString(i18n.language) : '—');

  const runDelete = async (ids: string[]) => {
    setDeleting(true);
    setErrorText('');
    let failed = 0;
    let lastError = '';
    for (const id of ids) {
      const item = archived.find((entry) => entry.team.id === id);
      try {
        await remove.mutateAsync({ teamId: id, expectedRevision: item?.team.revision });
      } catch (error) {
        failed += 1;
        lastError = apiErrorMessage(t, error);
      }
    }
    setDeleting(false);
    setPendingDelete(null);
    setSelected(new Set());
    if (failed > 0) setErrorText(t('archived.deleteFailed', { count: failed, reason: lastError }));
    else setNotice(t('archived.deleted', { count: ids.length }));
  };

  const pendingName =
    pendingDelete?.length === 1 ? (archived.find((item) => item.team.id === pendingDelete[0])?.team.name ?? '') : '';

  return (
    <section className="rounded-xl border border-neutral-200 bg-white p-5 dark:border-neutral-800 dark:bg-neutral-900">
      <div className="flex flex-wrap items-center gap-2">
        <input
          type="search"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder={t('archived.search')}
          aria-label={t('archived.search')}
          className="min-w-0 flex-1 rounded-md border border-neutral-300 bg-transparent px-3 py-1.5 text-sm dark:border-neutral-700"
        />
        <select
          value={sort}
          onChange={(event) => setSort(event.target.value as SortKey)}
          aria-label={t('archived.sort')}
          className="rounded-md border border-neutral-300 bg-transparent px-2 py-1.5 text-sm dark:border-neutral-700"
        >
          <option value="archivedAt">{t('archived.sortArchivedAt')}</option>
          <option value="createdAt">{t('archived.sortCreatedAt')}</option>
          <option value="name">{t('archived.sortName')}</option>
        </select>
      </div>

      {visible.length > 0 && (
        <div className="mt-3 flex items-center gap-3 border-b border-neutral-100 pb-2 text-sm dark:border-neutral-800">
          <label className="flex items-center gap-2 text-neutral-500">
            <input type="checkbox" checked={allVisibleSelected} onChange={toggleAll} className="h-4 w-4" />
            {t('archived.selectAll')}
          </label>
          {selected.size > 0 && (
            <button
              type="button"
              onClick={() => {
                setNotice('');
                setPendingDelete([...selected]);
              }}
              className="ml-auto rounded-md bg-red-600 px-3 py-1 text-xs font-medium text-white hover:bg-red-700"
            >
              {t('archived.deleteSelected', { count: selected.size })}
            </button>
          )}
        </div>
      )}

      {notice && (
        <p className="mt-3 text-sm text-green-600" role="status">
          {notice}
        </p>
      )}
      {errorText && (
        <p className="mt-3 text-sm text-red-600" role="alert">
          {errorText}
        </p>
      )}

      {teams.isPending && <p className="mt-3 text-sm text-neutral-500">{t('status.loading')}</p>}
      {teams.isSuccess && archived.length === 0 && <p className="mt-3 text-sm text-neutral-500">{t('archived.empty')}</p>}
      {archived.length > 0 && visible.length === 0 && <p className="mt-3 text-sm text-neutral-500">{t('archived.noMatch')}</p>}

      <ul className="mt-1 divide-y divide-neutral-100 dark:divide-neutral-800">
        {visible.map((item) => {
          const { team, bots } = item;
          const focused = team.id === focusId;
          return (
            <li
              key={team.id}
              ref={focused ? focusRef : undefined}
              className={`flex items-start gap-3 py-3 ${focused ? '-mx-2 rounded-lg bg-amber-50 px-2 dark:bg-amber-950/40' : ''}`}
            >
              <input
                type="checkbox"
                checked={selected.has(team.id)}
                onChange={() => toggle(team.id)}
                aria-label={t('archived.select', { name: team.name })}
                className="mt-1 h-4 w-4"
              />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{team.name}</p>
                <p className="truncate font-mono text-xs text-neutral-500">{team.directory}</p>
                <p className="mt-0.5 text-xs text-neutral-400">
                  {t('archived.meta', { members: bots.length, created: date(team.createdAt), archived: date(team.archivedAt) })}
                </p>
              </div>
              <div className="flex shrink-0 gap-1.5">
                <button
                  type="button"
                  onClick={() => {
                    setNotice('');
                    setErrorText('');
                    restore.mutate(
                      { teamId: team.id, expectedRevision: team.revision },
                      {
                        onSuccess: () => setNotice(t('archived.restored', { name: team.name })),
                        onError: (error) => setErrorText(apiErrorMessage(t, error)),
                      },
                    );
                  }}
                  disabled={restore.isPending || deleting}
                  className={buttonClass}
                >
                  {t('team.restore')}
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setNotice('');
                    setPendingDelete([team.id]);
                  }}
                  disabled={deleting}
                  className="rounded-md border border-red-200 px-2 py-1 text-xs text-red-600 hover:bg-red-50 disabled:opacity-50 dark:border-red-900 dark:hover:bg-red-950"
                >
                  {t('team.delete')}
                </button>
              </div>
            </li>
          );
        })}
      </ul>

      <ConfirmDialog
        open={pendingDelete !== null}
        title={
          pendingDelete && pendingDelete.length > 1
            ? t('archived.deleteManyTitle', { count: pendingDelete.length })
            : t('team.deleteConfirmTitle', { name: pendingName })
        }
        body={t('archived.deleteBody')}
        confirmLabel={t('team.delete')}
        pending={deleting}
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => {
          if (pendingDelete) void runDelete(pendingDelete);
        }}
      />
    </section>
  );
}
