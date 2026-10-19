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
import { Alert, Button, cx, Dialog, EmptyState, Fieldset, Icons, interactive, Skeleton } from '../../components/ui';
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

  // The picker's swatches: the drawn option on a quiet tile, selected with
  // the signal ring, hovered with the ink layer like every other choice.
  const tile = (selected: boolean) => cx('flex items-center justify-center rounded-xl p-1 transition-colors', selected ? cx(interactive.selected, 'ring-2 ring-signal-500') : interactive.hover);

  return (
    <Dialog
      open
      size="lg"
      onClose={onClose}
      busy={busy}
      closeLabel={t('common.close')}
      title={title}
      footer={
        <>
          {hasOwn && (
            <span className="mr-auto">
              <Button variant="ghost" icon={Icons.retry} onClick={() => onSave(null)} disabled={busy}>
                {t('avatar.useDefault')}
              </Button>
            </span>
          )}
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            {t('common.cancel')}
          </Button>
          {canSave && (
            <Button variant="primary" onClick={() => void save()} loading={busy}>
              {t('avatar.save')}
            </Button>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-6 sm:flex-row">
        <div className="flex shrink-0 flex-col items-center gap-3 sm:w-36">
          <div className="rounded-2xl bg-neutral-900/[0.03] p-4 dark:bg-white/[0.04]">
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
          <Button size="sm" icon={Icons.upload} onClick={() => fileInput.current?.click()} disabled={busy}>
            {t('avatar.usePicture')}
          </Button>
        </div>

        {!series || !parts || !draft ? (
          <div className="min-w-0 flex-1">
            {catalog.isPending ? <Skeleton className="h-40 w-full" /> : <EmptyState icon={<Icons.roles size={20} aria-hidden />} title={t('avatar.noCatalog')} />}
          </div>
        ) : (
          <div className="min-w-0 flex-1 space-y-5">
            <Fieldset legend={t('avatar.shape')}>
              <div role="radiogroup" aria-label={t('avatar.shape')} className="flex flex-wrap gap-1">
                {parts.shapes.map((shape) => (
                  <button key={shape} type="button" role="radio" aria-checked={composedSelected && draft.shape === shape} aria-label={shape} title={shape} onClick={() => compose({ shape })} className={tile(composedSelected && draft.shape === shape)}>
                    <AvatarImage src={draw({ ...draft, shape })} size={36} />
                  </button>
                ))}
              </div>
            </Fieldset>
            <Fieldset legend={t('avatar.eyes')}>
              <div role="radiogroup" aria-label={t('avatar.eyes')} className="flex flex-wrap gap-1">
                {parts.eyes.map((eyes) => (
                  <button key={eyes} type="button" role="radio" aria-checked={composedSelected && draft.eyes === eyes} aria-label={eyes} title={eyes} onClick={() => compose({ eyes })} className={tile(composedSelected && draft.eyes === eyes)}>
                    <AvatarImage src={draw({ ...draft, eyes })} size={36} />
                  </button>
                ))}
              </div>
            </Fieldset>
            <Fieldset legend={t('avatar.color')}>
              <div role="radiogroup" aria-label={t('avatar.color')} className="flex flex-wrap gap-2">
                {parts.palette.map((color) => (
                  <button
                    key={color}
                    type="button"
                    role="radio"
                    aria-checked={composedSelected && draft.color === color}
                    aria-label={color}
                    title={color}
                    onClick={() => compose({ color })}
                    className={cx('h-7 w-7 rounded-full transition-transform hover:scale-110', composedSelected && draft.color === color && 'ring-2 ring-signal-500 ring-offset-2 ring-offset-white dark:ring-offset-neutral-900')}
                    style={{ background: color }}
                  />
                ))}
              </div>
            </Fieldset>
          </div>
        )}
      </div>

      {shownError && (
        <div className="mt-4">
          <Alert tone="danger">{shownError}</Alert>
        </div>
      )}
    </Dialog>
  );
}