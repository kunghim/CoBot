// A team's daily token limit and each member's. What is not set here follows
// the default in the settings; every change applies at once.
//
// A member lives inside its team: its limit cannot be above the team's, and
// it cannot be unlimited while the team has a limit. The team's limit is also
// shared, so what a member may use is whatever of it the others left.
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useResetBotBudget, useResetTeamBudget, useSetBotBudget, useSetTeamBudget, useStatus, useTeamBudget } from '../../api/queries';
import type { BudgetLine, TeamWithBots } from '../../api/types';
import { Alert, Button, Dialog, Icons, NumberInput, SegmentedControl, Skeleton, useToast } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { defaultTokens, formatTokens, fromUnits, minUnits, scaleOf, stepOf, toUnits, unitLabelKey, unitSize, type BudgetKind, type BudgetScale } from '../budget/budgetUnits';

type Mode = 'follow' | 'custom' | 'none';

function modeOf(line: BudgetLine): Mode {
  if (line.override === null) return 'follow';
  return line.override === 0 ? 'none' : 'custom';
}

interface RowProps {
  title: string;
  kind: BudgetKind;
  scale: BudgetScale;
  line: BudgetLine;
  busy: boolean;
  /** What resetting this row also resets, shown under the button. */
  resetHint?: string;
  onSave: (limit: number | null) => void;
  onReset: () => void;
}

/** One limit: follow the default, set one, or none, and what today's spend is against it. */
function LimitRow({ title, kind, scale, line, busy, resetHint, onSave, onReset }: RowProps) {
  const { t, i18n } = useTranslation();
  const mode = modeOf(line);
  // A member can go no higher than its team's limit.
  const ceiling = kind === 'bot' && line.ceiling > 0 ? line.ceiling : 0;
  const capped = (tokens: number) => (ceiling > 0 ? Math.min(tokens, ceiling) : tokens);
  const current = capped(line.override && line.override > 0 ? line.override : line.default > 0 ? line.default : defaultTokens(kind, scale));
  const [draft, setDraft] = useState(toUnits(current, kind, scale));
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => setDraft(toUnits(current, kind, scale)), [current, kind, scale]);
  useEffect(() => () => window.clearTimeout(timer.current), []);

  const commit = (units: number) => {
    window.clearTimeout(timer.current);
    if (!Number.isFinite(units) || units < minUnits(kind)) return;
    const tokens = capped(fromUnits(units, kind, scale));
    if (tokens !== line.override) onSave(tokens);
  };
  const choose = (next: Mode) => {
    if (next === mode) return;
    onSave(next === 'follow' ? null : next === 'none' ? 0 : current);
  };
  const limitText = line.effective > 0 ? formatTokens(line.effective, i18n.language) : t('budget.unlimited');
  const maxUnits = ceiling > 0 ? Math.floor(ceiling / unitSize(kind, scale)) : undefined;

  return (
    <div className="space-y-2 py-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="min-w-0 truncate text-sm font-medium text-neutral-900 dark:text-neutral-50">{title}</span>
        <span className="text-xs text-neutral-500 dark:text-neutral-400">
          {t('budget.usedToday', { used: formatTokens(line.usedToday, i18n.language), limit: limitText })}
        </span>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <SegmentedControl
          label={title}
          size="md"
          value={mode}
          onChange={choose}
          options={[
            {
              value: 'follow',
              label: t('budget.follow', { limit: line.default > 0 ? formatTokens(line.default, i18n.language) : t('budget.unlimited') }),
              disabled: busy,
            },
            { value: 'custom', label: t('budget.custom'), disabled: busy },
            // Under a team with a limit, a member cannot be left without one.
            { value: 'none', label: kind === 'bot' ? t('budget.ownerNone') : t('budget.unlimited'), disabled: busy || ceiling > 0 },
          ]}
        />
        {mode === 'custom' && (
          <div className="w-44" onBlur={() => commit(draft)}>
            <NumberInput
              value={draft}
              min={minUnits(kind)}
              max={maxUnits}
              step={stepOf(kind)}
              unit={t(unitLabelKey(kind, scale))}
              decreaseLabel={t('common.decrease')}
              increaseLabel={t('common.increase')}
              disabled={busy}
              onChange={(next) => {
                setDraft(next);
                window.clearTimeout(timer.current);
                timer.current = window.setTimeout(() => commit(next), 700);
              }}
            />
          </div>
        )}
        <Button size="sm" icon={Icons.retry} disabled={busy || line.usedToday === 0} onClick={onReset} title={t('budget.resetHint')}>
          {t('budget.reset')}
        </Button>
      </div>
      {line.limitedBy === 'team' && (
        <p className="text-xs leading-5 text-neutral-500 dark:text-neutral-400">{t('budget.heldToTeam', { limit: formatTokens(line.effective, i18n.language) })}</p>
      )}
      {ceiling > 0 && <p className="text-xs leading-5 text-neutral-500 dark:text-neutral-400">{t('budget.unlimitedNeedsTeamNone')}</p>}
      {resetHint && <p className="text-xs leading-5 text-neutral-500 dark:text-neutral-400">{resetHint}</p>}
    </div>
  );
}

export default function TeamBudgetDialog({ item, onClose }: { item: TeamWithBots; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const { team, bots } = item;
  const budget = useTeamBudget(team.id);
  const scale = scaleOf(useStatus().data?.profile);
  const setTeam = useSetTeamBudget(team.id);
  const setBot = useSetBotBudget(team.id);
  const resetTeam = useResetTeamBudget(team.id);
  const resetBot = useResetBotBudget(team.id);
  const [errorText, setErrorText] = useState('');
  const busy = setTeam.isPending || setBot.isPending || resetTeam.isPending || resetBot.isPending;

  const fail = (error: unknown) => setErrorText(apiErrorMessage(t, error));
  const saved = (result?: { clamped?: number }) => {
    toast.show({ tone: 'success', title: t('settings.saved'), duration: 2000 });
    if (result?.clamped) toast.show({ tone: 'info', title: t('budget.clamped', { count: result.clamped }), duration: 4000 });
  };
  const resetDone = () => toast.show({ tone: 'success', title: t('budget.resetDone'), duration: 2000 });

  return (
    <Dialog
      open
      onClose={onClose}
      closeLabel={t('common.close')}
      title={t('budget.title', { name: team.name })}
      description={t('budget.dialogHint')}
      footer={<Button onClick={onClose}>{t('common.close')}</Button>}
      maxHeight="40rem"
    >
      {budget.isPending && <Skeleton className="h-24 w-full" />}
      {budget.isError && <Alert tone="danger">{apiErrorMessage(t, budget.error)}</Alert>}
      {errorText && (
        <div className="mb-2">
          <Alert tone="danger" onClose={() => setErrorText('')} closeLabel={t('common.close')}>
            {errorText}
          </Alert>
        </div>
      )}
      {budget.data && (
        <div className="divide-y divide-neutral-200 dark:divide-neutral-800">
          <LimitRow
            title={t('budget.team')}
            kind="team"
            scale={scale}
            line={budget.data.team}
            busy={busy}
            resetHint={t('budget.resetTeamHint')}
            onSave={(limit) => {
              setErrorText('');
              setTeam.mutate(limit, { onSuccess: saved, onError: fail });
            }}
            onReset={() => {
              setErrorText('');
              resetTeam.mutate(undefined, { onSuccess: resetDone, onError: fail });
            }}
          />
          {budget.data.members.map((member) => {
            const bot = bots.find((entry) => entry.id === member.botId);
            if (!bot) return null;
            return (
              <LimitRow
                key={member.botId}
                title={bot.name}
                kind="bot"
                scale={scale}
                line={member}
                busy={busy}
                onSave={(limit) => {
                  setErrorText('');
                  setBot.mutate({ botId: member.botId, limit }, { onSuccess: saved, onError: fail });
                }}
                onReset={() => {
                  setErrorText('');
                  resetBot.mutate(member.botId, { onSuccess: resetDone, onError: fail });
                }}
              />
            );
          })}
        </div>
      )}
    </Dialog>
  );
}
