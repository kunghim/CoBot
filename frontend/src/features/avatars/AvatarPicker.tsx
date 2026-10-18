// AvatarPicker chooses an avatar: compose one from a catalog series' parts
// (a shape, eyes and a color, every option drawn as the avatar it would
// make), or use a picture from this computer, cut to a centered square. The
// picture is kept in CoBot's data root on this computer and sent nowhere.
// "Use default" removes the choice, so the member or role follows its
// default again.
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import { useAvatarCatalog, useAvatarUpload } from '../../api/queries';
import type { AvatarRef } from '../../api/types';
import { useDialogFocus } from '../../components/useDialogFocus';
import { apiErrorMessage } from '../../i18n/errors';
import { AvatarImage } from './Avatar';
import { AVATAR_PICTURE_TYPES, AvatarPictureError, cropToAvatar, type CroppedPicture } from './crop';
import { FALLBACK_AVATAR, avatarRef, drawAvatar, seriesParts, type Composed, type ComposedAvatar } from './render';

type Picture = { kind: 'stored'; file: string } | { kind: 'new'; cropped: CroppedPicture };

export default function AvatarPicker({
  open,
  title,
  start,
  currentPicture,
  hasOwn,
  pending,
  errorText,
  onSave,
  onClose,
}: {
  open: boolean;
  title: string;
  /** The composed avatar to start composing from (the shown one if composed). */
  start: Composed | null;
  /** The local picture shown now, if that is the current choice. */
  currentPicture?: string;
  /** Whether there is a choice to remove. */
  hasOwn: boolean;
  pending: boolean;
  errorText: string;
  onSave: (avatar: AvatarRef | null) => void;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation();
  const catalog = useAvatarCatalog(open);
  const [uploading, setUploading] = useState(false);
  const busy = pending || uploading;
  const dialogRef = useDialogFocus(open, onClose, busy);
  const fileInput = useRef<HTMLInputElement>(null);
  const series = start?.series ?? null;
  const parts = series ? seriesParts(series) : null;
  const [draft, setDraft] = useState<ComposedAvatar | null>(start?.avatar ?? null);
  const [picture, setPicture] = useState<Picture | null>(currentPicture ? { kind: 'stored', file: currentPicture } : null);
  const [localError, setLocalError] = useState('');
  const stored = useAvatarUpload(picture?.kind === 'stored' ? picture.file : undefined);
  const startKey = `${start ? `${start.series.id}|${start.avatar.shape}|${start.avatar.eyes}|${start.avatar.color}` : ''}|${currentPicture ?? ''}`;
  useEffect(() => {
    if (!open) return;
    // Restart from the current avatar each time the picker opens.
    setDraft(start?.avatar ?? null);
    setPicture(currentPicture ? { kind: 'stored', file: currentPicture } : null);
    setLocalError('');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, startKey]);

  if (!open) return null;
  const draw = (avatar: ComposedAvatar) => (series ? drawAvatar(series, avatar) : '');
  const compose = (change: Partial<ComposedAvatar>) => {
    if (!draft) return;
    setDraft({ ...draft, ...change });
    setPicture(null);
  };
  const option = (selected: boolean) =>
    `flex items-center justify-center rounded-xl p-1 transition-colors ${
      selected ? 'bg-neutral-100 ring-2 ring-signal-500 dark:bg-neutral-800' : 'hover:bg-neutral-100 dark:hover:bg-neutral-800'
    }`;
  const composedSelected = !picture;
  const preview = picture?.kind === 'new' ? picture.cropped.preview : picture?.kind === 'stored' ? (stored.data ?? FALLBACK_AVATAR) : draft ? draw(draft) : FALLBACK_AVATAR;

  const pickPicture = async (file: File | undefined) => {
    if (!file) return;
    setLocalError('');
    try {
      setPicture({ kind: 'new', cropped: await cropToAvatar(file) });
    } catch (error) {
      setLocalError(t(error instanceof AvatarPictureError ? error.messageKey : 'avatar.pictureUnreadable'));
    }
  };

  const save = async () => {
    setLocalError('');
    if (picture?.kind === 'stored') return onSave({ kind: 'upload', file: picture.file });
    if (picture?.kind === 'new') {
      setUploading(true);
      try {
        const { file } = await api.saveAvatarUpload(picture.cropped.base64);
        onSave({ kind: 'upload', file });
      } catch (error) {
        setLocalError(apiErrorMessage(t, error));
      } finally {
        setUploading(false);
      }
      return;
    }
    if (series && draft) onSave(avatarRef(series, draft));
  };

  const canSave = !!picture || (!!series && !!draft);
  const shownError = localError || errorText;

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-neutral-950/45 p-4 pt-[8vh] backdrop-blur-[2px]" onMouseDown={() => !busy && onClose()}>
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

        <div className="mt-5 flex flex-col gap-6 sm:flex-row">
          <div className="flex shrink-0 flex-col items-center gap-3 sm:w-36">
            <div className="rounded-2xl bg-neutral-50 p-4 dark:bg-neutral-950">
              <AvatarImage src={preview} size={96} picture={!!picture} />
            </div>
            <p className="text-center text-xs text-neutral-500 dark:text-neutral-400">
              {picture ? t('avatar.pictureNote') : series ? t('avatar.series', { name: series.name[i18n.language] ?? series.name.en ?? series.id }) : ''}
            </p>
            <input
              ref={fileInput}
              type="file"
              accept={AVATAR_PICTURE_TYPES.join(',')}
              className="hidden"
              onChange={(event) => {
                void pickPicture(event.target.files?.[0]);
                event.target.value = '';
              }}
            />
            <button
              type="button"
              onClick={() => fileInput.current?.click()}
              disabled={busy}
              className="rounded-md border border-neutral-300 px-3 py-1.5 text-sm text-neutral-600 hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-neutral-800"
            >
              {t('avatar.usePicture')}
            </button>
          </div>

          {!series || !parts || !draft ? (
            <p className="min-w-0 flex-1 text-sm leading-6 text-neutral-600 dark:text-neutral-300">
              {catalog.isPending ? t('status.loading') : t('avatar.noCatalog')}
            </p>
          ) : (
            <div className="min-w-0 flex-1 space-y-5">
              <fieldset>
                <legend className="text-sm text-neutral-500 dark:text-neutral-400">{t('avatar.shape')}</legend>
                <div role="radiogroup" aria-label={t('avatar.shape')} className="mt-2 flex flex-wrap gap-1">
                  {parts.shapes.map((shape) => (
                    <button key={shape} type="button" role="radio" aria-checked={composedSelected && draft.shape === shape} aria-label={shape} title={shape} onClick={() => compose({ shape })} className={option(composedSelected && draft.shape === shape)}>
                      <AvatarImage src={draw({ ...draft, shape })} size={36} />
                    </button>
                  ))}
                </div>
              </fieldset>
              <fieldset>
                <legend className="text-sm text-neutral-500 dark:text-neutral-400">{t('avatar.eyes')}</legend>
                <div role="radiogroup" aria-label={t('avatar.eyes')} className="mt-2 flex flex-wrap gap-1">
                  {parts.eyes.map((eyes) => (
                    <button key={eyes} type="button" role="radio" aria-checked={composedSelected && draft.eyes === eyes} aria-label={eyes} title={eyes} onClick={() => compose({ eyes })} className={option(composedSelected && draft.eyes === eyes)}>
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
                      aria-checked={composedSelected && draft.color === color}
                      aria-label={color}
                      title={color}
                      onClick={() => compose({ color })}
                      className={`h-7 w-7 rounded-full transition-transform hover:scale-110 ${composedSelected && draft.color === color ? 'ring-2 ring-signal-500 ring-offset-2 ring-offset-white dark:ring-offset-neutral-900' : ''}`}
                      style={{ background: color }}
                    />
                  ))}
                </div>
              </fieldset>
            </div>
          )}
        </div>

        {shownError && <p className="mt-4 text-sm text-red-600" role="alert">{shownError}</p>}

        <div className="mt-6 flex items-center gap-2">
          {hasOwn && (
            <button type="button" onClick={() => onSave(null)} disabled={busy} className="rounded-md px-3 py-1.5 text-sm text-neutral-600 hover:text-neutral-900 disabled:opacity-50 dark:text-neutral-300 dark:hover:text-neutral-100">
              {t('avatar.useDefault')}
            </button>
          )}
          <span className="flex-1" />
          <button type="button" onClick={onClose} disabled={busy} className="rounded-md px-3 py-1.5 text-sm text-neutral-500 hover:text-neutral-900 disabled:opacity-50 dark:hover:text-neutral-100">
            {t('team.cancel')}
          </button>
          {canSave && (
            <button
              type="button"
              onClick={() => void save()}
              disabled={busy}
              className="rounded-md bg-neutral-900 px-4 py-1.5 text-sm font-medium text-white hover:bg-neutral-700 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
            >
              {busy ? t('status.loading') : t('avatar.save')}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
