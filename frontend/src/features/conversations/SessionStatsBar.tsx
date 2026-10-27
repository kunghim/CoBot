// SessionStatsBar is the line under the composer that sums up a member's
// conversation, like Codex's: turns and speed, tokens and cache, context in
// use, and the account's limits. Each item opens its details on hover or
// focus. The numbers are what the engine reported; nothing here calls the
// model.
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Gauge, Database, Timer } from 'lucide-react';
import { cx, interactive, Popover, ProgressRing } from '../../components/ui';
import { useBotStats } from '../../api/queries';
import type { SessionStats } from '../../api/types';

export function compactTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n >= 10_000_000 ? 0 : 1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(n >= 10_000 ? 0 : 1)}K`;
  return String(n);
}

export function percent(part: number, whole: number): number {
  return whole > 0 ? Math.round((part / whole) * 100) : 0;
}

interface Limit {
  usedPercent?: number;
  windowDurationMins?: number;
  resetsAt?: number;
}

// Each number opens its details; the panel is the library's Popover, so
// Escape, a click outside and the keyboard work as everywhere else.
function Item({ label, title, children, testId }: { label: ReactNode; title: string; children: ReactNode; testId: string }) {
  return (
    <Popover
      label={title}
      placement="top-start"
      className="w-64 p-3 text-xs text-neutral-700 dark:text-neutral-200"
      trigger={
        <button type="button" data-testid={testId} className={cx('inline-flex items-center gap-1 rounded-full px-2 py-0.5 transition-colors hover:text-neutral-900 dark:hover:text-neutral-100', interactive.hover)}>
          {label}
        </button>
      }
    >
      {children}
    </Popover>
  );
}

function Row({ name, value, strong = false }: { name: string; value: string; strong?: boolean }) {
  return (
    <span className="flex justify-between gap-3 py-0.5">
      <span className="text-neutral-500 dark:text-neutral-400">{name}</span>
      <span className={strong ? 'font-semibold tabular-nums' : 'font-medium tabular-nums'}>{value}</span>
    </span>
  );
}

/** The window closest to running out: the one that will stop the next turn
 * first. The weekly window is usually the emptier one, so showing it by
 * default hid a five-hour window that was about to block the account. */
export function tightestLimit<T extends { usedPercent?: number }>(limits: { primary?: T; secondary?: T }): T | undefined {
  const { primary, secondary } = limits;
  if (!primary) return secondary;
  if (!secondary) return primary;
  return (secondary.usedPercent ?? 0) > (primary.usedPercent ?? 0) ? secondary : primary;
}

export default function SessionStatsBar({ botId }: { botId: string }) {
  const { t } = useTranslation();
  const query = useBotStats(botId);
  const stats: SessionStats | undefined = query.data;
  if (!stats || stats.turns === 0) return null;
  const seconds = (ms: number) => t('stats.duration', { m: Math.floor(ms / 60000), s: Math.round((ms % 60000) / 1000) });
  const tokens = stats.tokens;
  const total = tokens.input + tokens.output;
  const hit = percent(tokens.cachedRead, tokens.input);
  const window = stats.context.window ?? 0;
  const used = percent(stats.context.used, window);
  const limits = (stats.rateLimits ?? {}) as { primary?: Limit; secondary?: Limit };
  const limitText = (limit?: Limit) => (limit?.usedPercent === undefined ? '—' : `${Math.round(limit.usedPercent)}%`);
  const resets = (limit?: Limit) => (limit?.resetsAt ? new Date(limit.resetsAt * 1000).toLocaleString() : '');
  // Nearly used up reads as a warning, and a window the next turn will
  // likely not fit in reads as an error.
  type Tone = 'danger' | 'warning' | 'neutral';
  const limitClass = (limit?: Limit) =>
    (limit?.usedPercent ?? 0) >= 95 ? 'text-red-600 dark:text-red-400' : (limit?.usedPercent ?? 0) >= 80 ? 'text-amber-600 dark:text-amber-400' : undefined;
  const limitTone = (limit?: Limit): Tone =>
    (limit?.usedPercent ?? 0) >= 95 ? 'danger' : (limit?.usedPercent ?? 0) >= 80 ? 'warning' : 'neutral';
  const tps = stats.outputTokensPerSecond ? Math.round(stats.outputTokensPerSecond) : 0;

  return (
    <div className="flex flex-wrap items-center gap-1 px-1 text-2xs text-neutral-500 dark:text-neutral-400" data-testid="session-stats">
      <Item testId="stats-turns" title={t('stats.timeTitle')} label={<><Timer size={12} aria-hidden="true" /><span className="font-mono tabular-nums">{t('stats.turns', { turns: stats.turns, steps: stats.steps })}</span>{tps > 0 && <span className="font-mono tabular-nums">{`· ${tps} tok/s`}</span>}</>}>
        <PopoverHead title={t('stats.timeTitle')} value={t('stats.turns', { turns: stats.turns, steps: stats.steps })} />
        <Row name={t('stats.modelTime')} value={seconds(stats.modelMs)} />
        <Row name={t('stats.toolTime')} value={seconds(stats.toolMs)} />
        {stats.firstStepMs ? <Row name={t('stats.firstToken')} value={`${(stats.firstStepMs / 1000).toFixed(1)}s`} /> : null}
        {tps > 0 && <Row name={t('stats.speed')} value={`${tps} tok/s`} />}
        <span className="mt-1.5 block text-2xs text-neutral-500 dark:text-neutral-400">{t('stats.estimateNote')}</span>
      </Item>
      <Item testId="stats-tokens" title={t('stats.tokensTitle')} label={<><Database size={12} aria-hidden="true" /><span className="font-mono tabular-nums">{compactTokens(total)} tok</span><span className="rounded-full bg-neutral-900/[0.06] px-1.5 py-px font-mono tabular-nums dark:bg-white/[0.08]">{t('stats.cacheHit', { percent: hit })}</span></>}>
        <PopoverHead title={t('stats.tokensTitle')} value={total.toLocaleString()} />
        <Row name={t('stats.output')} value={tokens.output.toLocaleString()} strong />
        {tokens.reasoning > 0 && <Row name={t('stats.reasoning')} value={tokens.reasoning.toLocaleString()} />}
        <div className="my-1.5">
          <MiniBar label={t('stats.cacheHitRate')} percent={hit} caption={`${hit}%`} />
        </div>
        <Row name={t('stats.cacheRead')} value={tokens.cachedRead.toLocaleString()} />
        <Row name={t('stats.cacheWrite')} value={tokens.cacheWrite.toLocaleString()} />
        <Row name={t('stats.uncached')} value={(tokens.input - tokens.cachedRead).toLocaleString()} />
      </Item>
      {window > 0 && (
        <Item testId="stats-context" title={t('stats.contextTitle', { percent: used })} label={<><ProgressRing value={used} label={t('stats.contextTitle', { percent: used })} size={14} showValue={false} tone={used >= 90 ? 'danger' : used >= 70 ? 'warning' : 'neutral'} /><span className="font-mono tabular-nums">{used}%</span></>}>
          <PopoverHead title={t('stats.contextTitle', { percent: used })} value={`${compactTokens(stats.context.used)} / ${compactTokens(window)}`} />
          <div className="my-1.5">
            <MiniBar label={t('stats.contextTitle', { percent: used })} percent={used} tone={used >= 90 ? 'danger' : used >= 70 ? 'warning' : 'neutral'} />
          </div>
          <span className="block text-2xs text-neutral-500 dark:text-neutral-400">{t('stats.contextNote')}</span>
        </Item>
      )}
      {(limits.primary || limits.secondary) && (
        <Item testId="stats-limits" title={t('stats.limitsTitle')} label={<span className={cx('inline-flex items-center gap-1', limitClass(tightestLimit(limits)))}><Gauge size={12} aria-hidden="true" /><span className="font-mono tabular-nums">{t('stats.limitShort', { percent: limitText(tightestLimit(limits)) })}</span></span>}>
          <PopoverHead title={t('stats.limitsTitle')} value={limitText(tightestLimit(limits))} />
          {limits.primary && (
            <div className="my-1">
              <MiniBar label={t('stats.limitWindow', { hours: Math.round((limits.primary.windowDurationMins ?? 0) / 60) })} percent={limits.primary.usedPercent ?? 0} caption={limitText(limits.primary)} tone={limitTone(limits.primary) ?? 'neutral'} />
              {limits.primary?.resetsAt ? <span className="mt-0.5 block text-2xs text-neutral-500 dark:text-neutral-400">{t('stats.resets', { time: resets(limits.primary) })}</span> : null}
            </div>
          )}
          {limits.secondary && (
            <div className="my-1">
              <MiniBar label={t('stats.limitWeek')} percent={limits.secondary.usedPercent ?? 0} caption={limitText(limits.secondary)} tone={limitTone(limits.secondary) ?? 'neutral'} />
              {limits.secondary?.resetsAt ? <span className="mt-0.5 block text-2xs text-neutral-500 dark:text-neutral-400">{t('stats.resets', { time: resets(limits.secondary) })}</span> : null}
            </div>
          )}
        </Item>
      )}
    </div>
  );
}

/** The panel's header line: what the panel is about and its headline number. */
function PopoverHead({ title, value }: { title: string; value: string }) {
  return (
    <span className="mb-1 flex items-baseline justify-between gap-3 border-b border-neutral-200 pb-1 font-semibold dark:border-neutral-700">
      <span>{title}</span>
      <span className="font-mono tabular-nums">{value}</span>
    </span>
  );
}

/** One proportion at a glance: a labelled bar with its share. Estimates stay
 * quiet; the caption carries the number. */
function MiniBar({ label, percent: value, caption, tone }: { label: string; percent: number; caption?: string; tone?: 'danger' | 'warning' | 'neutral' }) {
  return (
    <span className="block">
      <span className="flex items-baseline justify-between gap-3">
        <span className="min-w-0 truncate">{label}</span>
        {caption !== undefined && <span className="font-mono tabular-nums">{caption}</span>}
      </span>
      <span className="mt-1 block h-1.5 overflow-hidden rounded-full bg-neutral-200 dark:bg-neutral-700">
        <span
          className={tone === 'danger' ? 'block h-full bg-red-500' : tone === 'warning' ? 'block h-full bg-amber-500' : 'block h-full bg-signal-500'}
          style={{ width: `${Math.min(Math.max(value, 0), 100)}%` }}
        />
      </span>
    </span>
  );
}
