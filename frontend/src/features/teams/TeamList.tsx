import LoadMore from '../../components/LoadMore';
// TeamList is the left panel: the "+ new team" action plus the two-level
// team tree (leader first, then members). Selecting a member makes it the
// conversation target; the panel shows only data the API really returned.
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Archive, ChevronDown, ChevronRight, Plus, RotateCw, Trash2, Undo2, Users, X } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { useArchiveTeam, useDeleteBot, useDeleteTeam, useRestoreTeam, useRetryTeam, useTeams } from '../../api/queries';
import type { Bot, TeamWithBots } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import { useSelectedBot } from '../../app/SelectedBotContext';
import CreateTeamDialog from './CreateTeamDialog';
import TeamTools from './TeamTools';
import { findBotContext, orderBotsLeaderFirst, teamBadgeKey } from './teamUtils';

const DOT_CLASS: Record<string, string> = {
  idle: 'bg-neutral-300 dark:bg-neutral-600',
  queued: 'bg-sky-500',
  working: 'bg-emerald-500',
  waiting_approval: 'bg-amber-500',
  waiting_input: 'bg-orange-500',
  interrupted: 'bg-red-400',
  unknown: 'bg-neutral-400',
};

export default function TeamList({ onCollapse }: { onCollapse: () => void }) {
  const { t } = useTranslation();
  const { botId, setBotId } = useSelectedBot();
  const navigate = useNavigate();
  const teams = useTeams(true);
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  const [createOpen, setCreateOpen] = useState(false);

  const items = teams.data?.items ?? [];

  const selectedTeamId = botId ? findBotContext(items, botId)?.team.id : undefined;
  // A new selection reveals its team; later manual collapse remains respected.
  useEffect(() => {
    if (!selectedTeamId) return;
    setExpanded((prev) => prev.has(selectedTeamId) ? prev : new Set(prev).add(selectedTeamId));
  }, [botId, selectedTeamId]);

  const select = (id: string) => {
    setBotId(id);
    navigate('/');
  };

  const expand = (teamId: string) => {
    setExpanded((prev) => (prev.has(teamId) ? prev : new Set(prev).add(teamId)));
  };

  const toggle = (teamId: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(teamId)) next.delete(teamId);
      else next.add(teamId);
      return next;
    });
  };

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between px-3 py-3">
        <span className="rounded-full bg-neutral-900 px-2 py-0.5 text-xs font-semibold text-white dark:bg-neutral-100 dark:text-neutral-900">
          cobot
        </span>
        <button
          type="button"
          onClick={onCollapse}
          title={t('nav.toggleTeamList')}
          aria-label={t('nav.toggleTeamList')}
          aria-expanded={true}
          className="flex h-9 w-9 items-center justify-center rounded-md text-neutral-500 transition-colors hover:bg-neutral-200 hover:text-neutral-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-neutral-400 dark:text-neutral-400 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
        >
          <X size={18} aria-hidden="true" />
        </button>
      </div>

      <div className="px-3 pb-2">
        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          className="flex w-full items-center gap-2 rounded-md border border-dashed border-neutral-300 px-3 py-2 text-sm text-neutral-600 transition-colors hover:border-neutral-400 hover:text-neutral-900 dark:border-neutral-700 dark:text-neutral-300 dark:hover:border-neutral-500 dark:hover:text-neutral-100"
        >
          <Plus size={16} aria-hidden="true" />
          {t('team.new')}
        </button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
        {teams.isPending && (
          <p className="px-2 py-1 text-sm text-neutral-500 dark:text-neutral-400" role="status">
            {t('team.loading')}
          </p>
        )}
        {teams.isError && (
          <p className="px-2 py-1 text-sm text-red-600" role="alert">
            {t('team.loadFailed')}
          </p>
        )}
        {teams.isSuccess && items.length === 0 && (
          <p className="px-2 py-1 text-sm text-neutral-500 dark:text-neutral-400">{t('home.noTeams')}</p>
        )}
        <ul className="space-y-1">
          {items.map((item) => (
            <TeamRow
              key={item.team.id}
              item={item}
              expanded={expanded.has(item.team.id)}
              selectedBotId={botId}
              onToggle={() => toggle(item.team.id)}
              onSelect={select}
            />
          ))}
        </ul>
        <LoadMore page={teams} />
      </div>

      <CreateTeamDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(leaderBotId, teamId) => {
          setCreateOpen(false);
          expand(teamId);
          select(leaderBotId);
        }}
      />
    </div>
  );
}

function TeamRow({
  item,
  expanded,
  selectedBotId,
  onToggle,
  onSelect,
}: {
  item: TeamWithBots;
  expanded: boolean;
  selectedBotId: string | null;
  onToggle: () => void;
  onSelect: (botId: string) => void;
}) {
  const { t } = useTranslation();
  const { team, bots } = item;
  const badgeKey = teamBadgeKey(team.status);
  const leader = bots.find((bot) => bot.isLeader);
  const leaderDirectory = leader ? team.directory : '';
  // A team whose folder is gone stays visible, greyed and read-only.
  const missing = team.status === 'directory_missing';

  return (
    <li className={`rounded-md ${missing ? 'opacity-60' : ''}`}>
      <div className="flex flex-wrap items-center gap-1">
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={expanded}
          aria-label={expanded ? t('team.collapse', { name: team.name }) : t('team.expand', { name: team.name })}
          title={expanded ? t('team.collapse', { name: team.name }) : t('team.expand', { name: team.name })}
          className="flex h-6 w-6 shrink-0 items-center justify-center rounded text-neutral-400 hover:bg-neutral-200 hover:text-neutral-700 dark:hover:bg-neutral-800 dark:hover:text-neutral-200"
        >
          {expanded ? <ChevronDown size={14} aria-hidden="true" /> : <ChevronRight size={14} aria-hidden="true" />}
        </button>
        <span className="min-w-0 flex-1 truncate text-sm font-medium">{team.name}</span>
        {badgeKey && (
          <span
            className="shrink-0 rounded-full bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-800 dark:bg-amber-900 dark:text-amber-200"
            role="status"
            title={missing ? `${t(badgeKey)} · ${team.directory}` : t(badgeKey)}
          >
            {t(badgeKey)}
          </span>
        )}
        <TeamActions team={team} />
      </div>
      {expanded && (
        <ul className="ml-6 mt-0.5 space-y-0.5">
          {orderBotsLeaderFirst(bots).map((bot) => (
            <li key={bot.id} className="flex flex-wrap items-center">
              <span className="min-w-0 flex-1">
                <BotRow bot={bot} directory={bot.isLeader ? leaderDirectory : ''} selected={bot.id === selectedBotId} onSelect={onSelect} />
              </span>
              {!bot.isLeader && !missing && <DeleteMemberButton bot={bot} />}
            </li>
          ))}
        </ul>
      )}
      {expanded && <TeamTools key={team.id} item={item} />}
    </li>
  );
}

function BotRow({
  bot,
  directory,
  selected,
  onSelect,
}: {
  bot: Bot;
  directory: string;
  selected: boolean;
  onSelect: (botId: string) => void;
}) {
  const { t } = useTranslation();
  const statusLabel = t(`team.status.${bot.status}`, { defaultValue: t(`team.status.unknown`) });

  return (
    <button
      type="button"
      onClick={() => onSelect(bot.id)}
      aria-current={selected}
      title={directory || undefined}
      className={`flex w-full min-w-0 items-center gap-2 rounded px-2 py-1 text-left text-sm transition-colors ${
        selected
          ? 'bg-neutral-200 text-neutral-900 dark:bg-neutral-800 dark:text-neutral-100'
          : 'text-neutral-600 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800/60'
      }`}
    >
      <span
        role="img"
        aria-label={statusLabel}
        title={statusLabel}
        className={`inline-block h-2 w-2 shrink-0 rounded-full ${DOT_CLASS[bot.status] ?? DOT_CLASS.unknown}`}
      />
      <span className="min-w-0 flex-1 truncate">{bot.name}</span>
      {bot.isLeader && (
        <span className="flex shrink-0 items-center gap-1 text-[10px] text-neutral-400" title={t('team.leader')}>
          <Users size={11} aria-hidden="true" />
          {t('team.leader')}
        </span>
      )}
    </button>
  );
}

// TeamActions offers the real lifecycle verbs (B09): archive pauses dispatch
// and keeps work; restore resumes it; delete is reachable only after
// archiving and never touches user files.
function TeamActions({ team }: { team: TeamWithBots['team'] }) {
  const { t } = useTranslation();
  const archive = useArchiveTeam();
  const restore = useRestoreTeam();
  const remove = useDeleteTeam();
  const retry = useRetryTeam();
  const [errorText, setErrorText] = useState('');
  const fail = (error: unknown) => setErrorText(apiErrorMessage(t, error));

  return (
    <>
      {/* A refusal gets its own full-width line: inside the240px header row
          it would be truncated (review C re-review). */}
      {errorText && (
        <span role="alert" className="w-full text-[10px] leading-4 text-red-600 dark:text-red-400">
          {errorText}
        </span>
      )}
      <span className="flex shrink-0 items-center gap-0.5">
      {team.status === 'error' && (
        <button
          type="button"
          onClick={() => {
            setErrorText('');
            retry.mutate({ teamId: team.id, expectedRevision: team.revision }, { onError: fail });
          }}
          title={t('team.retry')}
          aria-label={t('team.retry')}
          className="flex h-6 w-6 items-center justify-center rounded text-neutral-400 hover:bg-neutral-200 hover:text-neutral-700 dark:hover:bg-neutral-800 dark:hover:text-neutral-200"
        >
          <RotateCw size={12} aria-hidden="true" />
        </button>
      )}
      {team.status === 'active' && (
        <button
          type="button"
          onClick={() => {
            setErrorText('');
            archive.mutate({ teamId: team.id, expectedRevision: team.revision }, { onError: fail });
          }}
          title={t('team.archive')}
          aria-label={t('team.archive')}
          className="flex h-6 w-6 items-center justify-center rounded text-neutral-400 hover:bg-neutral-200 hover:text-neutral-700 dark:hover:bg-neutral-800 dark:hover:text-neutral-200"
        >
          <Archive size={12} aria-hidden="true" />
        </button>
      )}
      {team.status === 'archived' && (
        <>
          <button
            type="button"
            onClick={() => {
              setErrorText('');
              restore.mutate({ teamId: team.id, expectedRevision: team.revision }, { onError: fail });
            }}
            title={t('team.restore')}
            aria-label={t('team.restore')}
            className="flex h-6 w-6 items-center justify-center rounded text-neutral-400 hover:bg-neutral-200 hover:text-neutral-700 dark:hover:bg-neutral-800 dark:hover:text-neutral-200"
          >
            <Undo2 size={12} aria-hidden="true" />
          </button>
          <button
            type="button"
            onClick={() => {
              setErrorText('');
              remove.mutate({ teamId: team.id, expectedRevision: team.revision }, { onError: fail });
            }}
            title={t('team.delete')}
            aria-label={t('team.delete')}
            className="flex h-6 w-6 items-center justify-center rounded text-neutral-400 hover:bg-red-100 hover:text-red-700 dark:hover:bg-red-950 dark:hover:text-red-300"
          >
            <Trash2 size={12} aria-hidden="true" />
          </button>
        </>
      )}
      </span>
    </>
  );
}

// DeleteMemberButton removes a member that carries no unfinished work (B08).
// A server refusal (unfinished tasks, runs, approvals) is shown inline: the
// most common outcome must never be a silent no-op (review C).
function DeleteMemberButton({ bot }: { bot: Bot }) {
  const { t } = useTranslation();
  const remove = useDeleteBot();
  const [errorText, setErrorText] = useState('');
  return (
    <>
      <button
        type="button"
        onClick={(event) => {
          event.stopPropagation();
          setErrorText('');
          remove.mutate({ botId: bot.id, expectedRevision: bot.revision }, { onError: (error) => setErrorText(apiErrorMessage(t, error)) });
        }}
        disabled={remove.isPending}
        title={t('team.deleteMember')}
        aria-label={t('team.deleteMember')}
        className="flex h-5 w-5 shrink-0 items-center justify-center rounded text-neutral-300 hover:bg-red-100 hover:text-red-700 disabled:opacity-50 dark:text-neutral-600 dark:hover:bg-red-950 dark:hover:text-red-300"
      >
        <Trash2 size={11} aria-hidden="true" />
      </button>
      {errorText && (
        <span role="alert" className="w-full text-[10px] leading-4 text-red-600 dark:text-red-400">
          {errorText}
        </span>
      )}
    </>
  );
}
