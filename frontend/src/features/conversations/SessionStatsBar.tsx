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

function Row({ name, value }: { name: string; value: string }) {
  return (
    <span className="flex justify-between gap-3 py-0.5">
      <span className="text-neutral-500 dark:text-neutral-400">{name}</span>
      <span className="font-medium tabular-nums">{value}</span>
    </span>
  );
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
  const tps = stats.outputTokensPerSecond ? Math.round(stats.outputTokensPerSecond) : 0;

  return (
    <div className="flex flex-wrap items-center gap-1 px-1 font-mono text-[11px] tracking-tight text-neutral-500 dark:text-neutral-400" data-testid="session-stats">
      <Item testId="stats-turns" title={t('stats.timeTitle')} label={<><Timer size={12} aria-hidden="true" />{t('stats.turns', { turns: stats.turns, steps: stats.steps })}{tps > 0 && ` · ${tps} tok/s`}</>}>
        <span className="mb-1 block font-semibold">{t('stats.timeTitle')}</span>
        <Row name={t('stats.modelTime')} value={seconds(stats.modelMs)} />
        <Row name={t('stats.toolTime')} value={seconds(stats.toolMs)} />
        {stats.firstStepMs ? <Row name={t('stats.firstToken')} value={`${(stats.firstStepMs / 1000).toFixed(1)}s`} /> : null}
        {tps > 0 && <Row name={t('stats.speed')} value={`${tps} tok/s`} />}
        <span className="mt-1 block text-[10px] text-neutral-500 dark:text-neutral-400">{t('stats.estimateNote')}</span>
      </Item>
      <Item testId="stats-tokens" title={t('stats.tokensTitle')} label={<><Database size={12} aria-hidden="true" />{compactTokens(total)} tok · {t('stats.cacheHit', { percent: hit })}</>}>
        <span className="mb-1 flex justify-between font-semibold"><span>{t('stats.tokensTitle')}</span><span className="tabular-nums">{total.toLocaleString()}</span></span>
        <Row name={t('stats.cacheHitRate')} value={`${hit}%`} />
        <Row name={t('stats.uncached')} value={(tokens.input - tokens.cachedRead).toLocaleString()} />
        <Row name={t('stats.cacheRead')} value={tokens.cachedRead.toLocaleString()} />
        <Row name={t('stats.cacheWrite')} value={tokens.cacheWrite.toLocaleString()} />
        <Row name={t('stats.output')} value={tokens.output.toLocaleString()} />
        {tokens.reasoning > 0 && <Row name={t('stats.reasoning')} value={tokens.reasoning.toLocaleString()} />}
      </Item>
      {window > 0 && (
        <Item testId="stats-context" title={t('stats.contextTitle', { percent: used })} label={<><ProgressRing value={used} label={t('stats.contextTitle', { percent: used })} size={14} showValue={false} tone={used >= 90 ? 'danger' : used >= 70 ? 'warning' : 'neutral'} />{used}%</>}>
          <span className="mb-1 flex justify-between font-semibold"><span>{t('stats.contextTitle', { percent: used })}</span><span className="tabular-nums">{compactTokens(stats.context.used)} / {compactTokens(window)}</span></span>
          <span className="block h-1.5 overflow-hidden rounded-full bg-neutral-200 dark:bg-neutral-700"><span className="block h-full bg-signal-500" style={{ width: `${Math.min(used, 100)}%` }} /></span>
          <span className="mt-1.5 block text-[10px] text-neutral-500 dark:text-neutral-400">{t('stats.contextNote')}</span>
        </Item>
      )}
      {(limits.primary || limits.secondary) && (
        <Item testId="stats-limits" title={t('stats.limitsTitle')} label={<><Gauge size={12} aria-hidden="true" />{t('stats.limitShort', { percent: limitText(limits.secondary ?? limits.primary) })}</>}>
          <span className="mb-1 block font-semibold">{t('stats.limitsTitle')}</span>
          {limits.primary && <Row name={t('stats.limitWindow', { hours: Math.round((limits.primary.windowDurationMins ?? 0) / 60) })} value={limitText(limits.primary)} />}
          {limits.primary?.resetsAt ? <span className="block text-[10px] text-neutral-500 dark:text-neutral-400">{t('stats.resets', { time: resets(limits.primary) })}</span> : null}
          {limits.secondary && <Row name={t('stats.limitWeek')} value={limitText(limits.secondary)} />}
          {limits.secondary?.resetsAt ? <span className="block text-[10px] text-neutral-500 dark:text-neutral-400">{t('stats.resets', { time: resets(limits.secondary) })}</span> : null}
        </Item>
      )}
    </div>
  );
}
