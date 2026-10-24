import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { useDeleteTeam, useRestoreTeam, useTeams } from '../../api/queries';
import type { TeamWithBots } from '../../api/types';
import { Alert, AlertDialog, Button, Card, Checkbox, cx, EmptyState, Icons, SearchInput, Select, Skeleton, useToast } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';

type SortKey = 'archivedAt' | 'createdAt' | 'name';

/**
 * ArchivedTeamsSection lists the teams set aside by archiving. Archiving
 * only succeeds for an idle team, so any of them can be restored to the
 * sidebar or deleted outright, one at a time or several at once. A team id
 * in ?team= (from the new-team dialog) is highlighted.
 */
export default function ArchivedTeamsSection({ onBusyChange }: { onBusyChange?: (busy: boolean) => void }) {
  const { t, i18n } = useTranslation();
  const toast = useToast();
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
  const [errorText, setErrorText] = useState('');
  useEffect(() => { onBusyChange?.(deleting || restore.isPending); return () => onBusyChange?.(false); }, [deleting, restore.isPending, onBusyChange]);
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
  const someVisibleSelected = !allVisibleSelected && visible.some((item) => selected.has(item.team.id));
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
    else toast.show({ tone: 'success', title: t('archived.deleted', { count: ids.length }) });
  };

  const pendingName =
    pendingDelete?.length === 1 ? (archived.find((item) => item.team.id === pendingDelete[0])?.team.name ?? '') : '';

  return (
    <Card>
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput
          className="min-w-0 flex-1"
          value={search}
          onChange={setSearch}
          placeholder={t('archived.search')}
          label={t('archived.search')}
          clearLabel={t('common.clearSearch')}
        />
        <div className="w-40">
          <Select
            label={t('archived.sort')}
            value={sort}
            onChange={(value) => setSort(value as SortKey)}
            options={[
              { value: 'archivedAt', label: t('archived.sortArchivedAt') },
              { value: 'createdAt', label: t('archived.sortCreatedAt') },
              { value: 'name', label: t('archived.sortName') },
            ]}
          />
        </div>
      </div>

      {visible.length > 0 && (
        <div className="mt-3 flex min-h-8 items-center gap-3 border-b border-neutral-200 pb-2 dark:border-neutral-800">
          <Checkbox checked={allVisibleSelected} indeterminate={someVisibleSelected} onChange={toggleAll} label={t('archived.selectAll')} />
          {selected.size > 0 && (
            <div className="ml-auto">
              <Button size="sm" variant="danger" icon={Icons.delete} onClick={() => setPendingDelete([...selected])}>
                {t('archived.deleteSelected', { count: selected.size })}
              </Button>
            </div>
          )}
        </div>
      )}

      {errorText && (
        <div className="mt-3">
          <Alert tone="danger" onClose={() => setErrorText('')} closeLabel={t('common.close')}>{errorText}</Alert>
        </div>
      )}

      {teams.isPending && <Skeleton className="mt-3 h-14 w-full" />}
      {teams.isSuccess && archived.length === 0 && <EmptyState icon={<Icons.archive size={20} aria-hidden />} title={t('archived.empty')} />}
      {archived.length > 0 && visible.length === 0 && <EmptyState icon={<Icons.search size={20} aria-hidden />} title={t('archived.noMatch')} />}

      <ul className="mt-1 divide-y divide-neutral-200 dark:divide-neutral-800">
        {visible.map((item) => {
          const { team, bots } = item;
          const focused = team.id === focusId;
          return (
            <li
              key={team.id}
              ref={focused ? focusRef : undefined}
              className={cx('flex items-start gap-3 py-3', focused && '-mx-2 rounded-lg bg-signal-500/[0.06] px-2')}
            >
              <span className="pt-0.5">
                <Checkbox checked={selected.has(team.id)} onChange={() => toggle(team.id)} label={<span className="sr-only">{t('archived.select', { name: team.name })}</span>} />
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{team.name}</p>
                <p className="truncate font-mono text-xs text-neutral-500 dark:text-neutral-400">{team.directory}</p>
                <p className="mt-0.5 text-xs text-neutral-500 dark:text-neutral-400">
                  {t('archived.meta', { members: bots.length, created: date(team.createdAt), archived: date(team.archivedAt) })}
                </p>
              </div>
              <div className="flex shrink-0 gap-1.5">
                <Button
                  size="sm"
                  icon={Icons.restore}
                  onClick={() => {
                    setErrorText('');
                    restore.mutate(
                      { teamId: team.id, expectedRevision: team.revision },
                      {
                        onSuccess: () => toast.show({ tone: 'success', title: t('archived.restored', { name: team.name }) }),
                        onError: (error) => setErrorText(apiErrorMessage(t, error)),
                      },
                    );
                  }}
                  disabled={restore.isPending || deleting}
                >
                  {t('team.restore')}
                </Button>
                <Button size="sm" variant="danger" onClick={() => setPendingDelete([team.id])} disabled={deleting}>
                  {t('team.delete')}
                </Button>
              </div>
            </li>
          );
        })}
      </ul>

      <AlertDialog
        open={pendingDelete !== null}
        title={
          pendingDelete && pendingDelete.length > 1
            ? t('archived.deleteManyTitle', { count: pendingDelete.length })
            : t('team.deleteConfirmTitle', { name: pendingName })
        }
        description={t('archived.deleteBody')}
        confirmLabel={t('team.delete')}
        cancelLabel={t('common.cancel')}
        busy={deleting}
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => {
          if (pendingDelete) void runDelete(pendingDelete);
        }}
      />
    </Card>
  );
}
