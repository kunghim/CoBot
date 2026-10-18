// FileDrop: pick a file from this computer by clicking or dropping. Says
// which kinds it accepts; whatever happens to the file is said near it.
import { useId, useRef, useState, type ReactNode } from 'react';
import { Upload } from 'lucide-react';
import { cx } from './foundation';

export function FileDrop({ accept, onFile, title, hint, disabled = false }: { accept: string; onFile: (file: File) => void; title: ReactNode; hint?: ReactNode; disabled?: boolean }) {
  const id = useId();
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  return (
    <div
      onDragOver={(event) => {
        if (disabled) return;
        event.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(event) => {
        event.preventDefault();
        setOver(false);
        const file = event.dataTransfer.files[0];
        if (file && !disabled) onFile(file);
      }}
      className={cx(
        'flex flex-col items-center gap-2 rounded-xl border-2 border-dashed px-6 py-8 text-center transition-colors',
        over ? 'border-signal-500 bg-signal-500/[0.05]' : 'border-neutral-300 dark:border-neutral-700',
        disabled && 'opacity-50',
      )}
    >
      <Upload size={20} aria-hidden className="text-neutral-400" />
      <label htmlFor={id} className={cx('text-sm font-medium text-neutral-800 dark:text-neutral-100', disabled ? 'cursor-not-allowed' : 'cursor-pointer underline-offset-4 hover:underline')}>
        {title}
      </label>
      {hint && <p className="text-xs text-neutral-500 dark:text-neutral-400">{hint}</p>}
      <input
        ref={input}
        id={id}
        type="file"
        accept={accept}
        disabled={disabled}
        className="sr-only"
        onChange={(event) => {
          const file = event.target.files?.[0];
          if (file) onFile(file);
          event.target.value = '';
        }}
      />
    </div>
  );
}
