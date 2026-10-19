// TeamList is the left panel: the "+ new team" action plus the two-level
// team tree (leader first, then members). Selecting a member makes it the
// conversation target; the panel shows only data the API really returned.
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useTeams } from '../../api/queries';
import type { Bot, TeamWithBots } from '../../api/types';
import { useSelectedBot } from '../../app/SelectedBotContext';
import LoadMore from '../../components/LoadMore';
import { Alert, Badge, Button, cx, EmptyState, Icons, interactive, MemberRow, PanelToggle, Skeleton } from '../../components/ui';
import CreateTeamDialog from './CreateTeamDialog';
import TeamMenu from './TeamMenu';
import { findBotContext, orderBotsLeaderFirst, teamBadgeKey } from './teamUtils';
import Avatar from '../avatars/Avatar';

// States that want the user: the row carries a dot that stays loud on purpose.
const ATTENTION = new Set(['waiting_approval', 'waiting_input']);

/** onClose, when given, puts a close toggle in the header (narrow windows,
 * where the list covers the conversation and its header toggle). */
export default function TeamList({ onClose }: { onClose?: () => void }) {
  const { t } = useTranslation();
  const { botId, setBotId } = useSelectedBot();
  const navigate = useNavigate();
  const teams = useTeams(true);
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  const [createOpen, setCreateOpen] = useState(false);

  // Archived teams live in Settings; the sidebar lists teams in use.
  const items = (teams.data?.items ?? []).filter((item) => item.team.status !== 'archived');

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
      <div className="flex min-h-14 items-center justify-between px-3 py-2">
        <span className="px-1 text-[11px] font-semibold uppercase tracking-[0.14em] text-neutral-500 dark:text-neutral-400">
          {t('nav.teamList')}
        </span>
        {onClose && <PanelToggle side="left" open onToggle={onClose} openLabel={t('nav.openTeamList')} closeLabel={t('nav.closeTeamList')} />}
      </div>

      <div className="px-3 pb-2">
        <Button block icon={Icons.add} onClick={() => setCreateOpen(true)}>
          {t('team.new')}
        </Button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
        {teams.isPending && (
          <div className="space-y-2 px-2 py-1" role="status" aria-label={t('team.loading')}>
            <Skeleton className="h-5 w-2/3" />
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
          </div>
        )}
        {teams.isError && (
          <div className="px-1">
            <Alert tone="danger">{t('team.loadFailed')}</Alert>
          </div>
        )}
        {teams.isSuccess && items.length === 0 && <EmptyState icon={<Icons.team size={20} aria-hidden />} title={t('home.noTeams')} />}
        <ul className="space-y-3">
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
  // A team whose folder is gone stays visible, greyed and read-only.
  const missing = team.status === 'directory_missing';
  const Chevron = expanded ? Icons.expand : Icons.next;

  return (
    <li className={missing ? 'opacity-60' : undefined}>
      <div className="flex items-center gap-1">
        {/* The whole row toggles the team; only the trailing menu does
            something else. */}
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={expanded}
          title={expanded ? t('team.collapse', { name: team.name }) : t('team.expand', { name: team.name })}
          className={cx('flex min-w-0 flex-1 items-center gap-1 rounded-lg py-1 pr-1 text-left transition-colors', interactive.hover)}
        >
          <span className="flex h-5 w-5 shrink-0 items-center justify-center text-neutral-400" aria-hidden="true">
            <Chevron size={14} />
          </span>
          <span className="min-w-0 flex-1 truncate text-[13px] font-semibold text-neutral-800 dark:text-neutral-100">{team.name}</span>
          {badgeKey && (
            <span title={missing ? `${t(badgeKey)} · ${team.directory}` : t(badgeKey)}>
              <Badge tone="warning">{t(badgeKey)}</Badge>
            </span>
          )}
        </button>
        <TeamMenu item={item} />
      </div>
      {expanded && (
        <ul className="mt-1 space-y-px">
          {orderBotsLeaderFirst(bots).map((bot) => (
            <li key={bot.id}>
              <BotRow bot={bot} directory={bot.isLeader ? team.directory : ''} selected={bot.id === selectedBotId} onSelect={onSelect} />
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}

function BotRow({ bot, directory, selected, onSelect }: { bot: Bot; directory: string; selected: boolean; onSelect: (botId: string) => void }) {
  const { t, i18n } = useTranslation();
  const statusLabel = t(`team.status.${bot.status}`, { defaultValue: t(`team.status.unknown`) });
  const busy = bot.status !== 'idle';
  const last = bot.lastMessage;
  return (
    <span title={directory || undefined} className="block">
      <MemberRow
        // The avatar shows the state (moving while working, wide eyes while
        // waiting); a dot stays only for waiting on the user.
        avatar={<Avatar bot={bot} showState />}
        name={
          <>
            {bot.name}
            {/* The leader's avatar marks it; screen readers hear it. */}
            {bot.isLeader && <span className="sr-only">{t('team.leader')}</span>}
          </>
        }
        statusLabel={statusLabel}
        status={busy ? statusLabel : undefined}
        time={last ? <time dateTime={last.createdAt}>{shortTime(last.createdAt, i18n.language)}</time> : undefined}
        preview={last ? (last.kind === 'user' ? `${t('team.previewYou')}${last.text}` : last.text) : undefined}
        current={selected}
        attention={ATTENTION.has(bot.status)}
        onSelect={() => onSelect(bot.id)}
      />
    </span>
  );
}

/** Today's messages show the time; older ones the date. */
function shortTime(value: string, language: string): string {
  const date = new Date(value);
  const today = new Date();
  return date.toDateString() === today.toDateString()
    ? date.toLocaleTimeString(language, { hour: '2-digit', minute: '2-digit' })
    : date.toLocaleDateString(language, { month: 'numeric', day: 'numeric' });
}
