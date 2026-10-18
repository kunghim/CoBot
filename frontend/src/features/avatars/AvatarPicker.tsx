// AvatarPicker composes an avatar from a catalog series' parts: a shape,
// eyes and a color, with a live preview. Every option is drawn as the avatar
// it would make, so choosing is looking, not reading names. "Use default"
// removes the choice, so the member or role follows its default again.
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAvatarCatalog } from '../../api/queries';
import type { AvatarRef, AvatarSeries } from '../../api/types';
import { useDialogFocus } from '../../components/useDialogFocus';
import { AvatarImage } from './Avatar';
import { avatarRef, drawAvatar, seriesParts, type ComposedAvatar } from './render';

export default function AvatarPicker({
  open,
  title,
  current,
  hasOwn,
  pending,
  errorText,
  onSave,
  onClose,
}: {
  open: boolean;
  title: string;
  /** What the avatar looks like now (own, role's or default), to start from. */
  current: { series: AvatarSeries; avatar: ComposedAvatar } | null;
  /** Whether there is a choice to remove. */
  hasOwn: boolean;
  pending: boolean;
  errorText: string;
  onSave: (avatar: AvatarRef | null) => void;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation();
  const catalog = useAvatarCatalog(open);
  const dialogRef = useDialogFocus(open, onClose, pending);
  const series = current?.series ?? null;
  const parts = series ? seriesParts(series) : null;
  const [draft, setDraft] = useState<ComposedAvatar | null>(current?.avatar ?? null);
  const startKey = current ? `${current.series.id}|${current.avatar.shape}|${current.avatar.eyes}|${current.avatar.color}` : '';
  useEffect(() => {
    if (open) setDraft(current?.avatar ?? null);
    // Restart from the current avatar each time the picker opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, startKey]);

  if (!open) return null;
  const draw = (avatar: ComposedAvatar) => (series ? drawAvatar(series, avatar) : '');
  const option = (selected: boolean) =>
    `flex items-center justify-center rounded-xl p-1 transition-colors ${
      selected ? 'bg-neutral-100 ring-2 ring-signal-500 dark:bg-neutral-800' : 'hover:bg-neutral-100 dark:hover:bg-neutral-800'
    }`;

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-neutral-950/45 p-4 pt-[8vh] backdrop-blur-[2px]" onMouseDown={() => !pending && onClose()}>
      <div
        ref={dialogRef as React.RefObject<HTMLDivElement>}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-labelledby="avatar-picker-title"
        onMouseDown={(event) => event.stopPropagation()}
        className="w-full max-w-2xl rounded-2xl border border-neutral-200 bg-white p-6 shadow-2xl dark:border-neutral-700 dark:bg-neutral-900"
      >
        <h2 id="avatar-picker-title" className="text-lg font-semibold tracking-tight">{title}</h2>

        {!series || !parts || !draft ? (
          <p className="mt-4 text-sm leading-6 text-neutral-600 dark:text-neutral-300">
            {catalog.isPending ? t('status.loading') : t('avatar.noCatalog')}
          </p>
        ) : (
          <div className="mt-5 flex flex-col gap-6 sm:flex-row">
            <div className="flex shrink-0 flex-col items-center gap-2 sm:w-36">
              <div className="rounded-2xl bg-neutral-50 p-4 dark:bg-neutral-950">
                <AvatarImage src={draw(draft)} size={96} />
              </div>
              <p className="text-xs text-neutral-500 dark:text-neutral-400">{t('avatar.series', { name: series.name[i18n.language] ?? series.name.en ?? series.id })}</p>
            </div>
            <div className="min-w-0 flex-1 space-y-5">
              <fieldset>
                <legend className="text-sm text-neutral-500 dark:text-neutral-400">{t('avatar.shape')}</legend>
                <div role="radiogroup" aria-label={t('avatar.shape')} className="mt-2 flex flex-wrap gap-1">
                  {parts.shapes.map((shape) => (
                    <button key={shape} type="button" role="radio" aria-checked={draft.shape === shape} aria-label={shape} title={shape} onClick={() => setDraft({ ...draft, shape })} className={option(draft.shape === shape)}>
                      <AvatarImage src={draw({ ...draft, shape })} size={36} />
                    </button>
                  ))}
                </div>
              </fieldset>
              <fieldset>
                <legend className="text-sm text-neutral-500 dark:text-neutral-400">{t('avatar.eyes')}</legend>
                <div role="radiogroup" aria-label={t('avatar.eyes')} className="mt-2 flex flex-wrap gap-1">
                  {parts.eyes.map((eyes) => (
                    <button key={eyes} type="button" role="radio" aria-checked={draft.eyes === eyes} aria-label={eyes} title={eyes} onClick={() => setDraft({ ...draft, eyes })} className={option(draft.eyes === eyes)}>
                      <AvatarImage src={draw({ ...draft, eyes })} size={36} />
                    </button>
                  ))}
                </div>
              </fieldset>
              <fieldset>
                <legend className="text-sm text-neutral-500 dark:text-neutral-400">{t('avatar.color')}</legend>
                <div role="radiogroup" aria-label={t('avatar.color')} className="mt-2 flex flex-wrap gap-2">
                  {parts.palette.map((color) => (
                    <button
                      key={color}
                      type="button"
                      role="radio"
                      aria-checked={draft.color === color}
                      aria-label={color}
                      title={color}
                      onClick={() => setDraft({ ...draft, color })}
                      className={`h-7 w-7 rounded-full transition-transform hover:scale-110 ${draft.color === color ? 'ring-2 ring-signal-500 ring-offset-2 ring-offset-white dark:ring-offset-neutral-900' : ''}`}
                      style={{ background: color }}
                    />
                  ))}
                </div>
              </fieldset>
            </div>
          </div>
        )}

        {errorText && <p className="mt-4 text-sm text-red-600" role="alert">{errorText}</p>}

        <div className="mt-6 flex items-center gap-2">
          {hasOwn && (
            <button type="button" onClick={() => onSave(null)} disabled={pending} className="rounded-md px-3 py-1.5 text-sm text-neutral-600 hover:text-neutral-900 disabled:opacity-50 dark:text-neutral-300 dark:hover:text-neutral-100">
              {t('avatar.useDefault')}
            </button>
          )}
          <span className="flex-1" />
          <button type="button" onClick={onClose} disabled={pending} className="rounded-md px-3 py-1.5 text-sm text-neutral-500 hover:text-neutral-900 disabled:opacity-50 dark:hover:text-neutral-100">
            {t('team.cancel')}
          </button>
          {series && draft && (
            <button
              type="button"
              onClick={() => onSave(avatarRef(series, draft))}
              disabled={pending}
              className="rounded-md bg-neutral-900 px-4 py-1.5 text-sm font-medium text-white hover:bg-neutral-700 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
            >
              {pending ? t('status.loading') : t('avatar.save')}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
