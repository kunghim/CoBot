// Slider: a value in a range where the exact number matters less than the
// position (a budget share, a zoom). Shows its value; pair with NumberInput
// when people need to type an exact number.
import { useId } from 'react';

export function Slider({ label, value, onChange, min = 0, max = 100, step = 1, format = String, disabled = false }: { label: string; value: number; onChange: (value: number) => void; min?: number; max?: number; step?: number; format?: (value: number) => string; disabled?: boolean }) {
  const id = useId();
  const percent = ((value - min) / (max - min)) * 100;
  return (
    <div className={disabled ? 'opacity-50' : undefined}>
      <div className="flex items-center justify-between text-sm">
        <label htmlFor={id} className="font-medium text-neutral-800 dark:text-neutral-100">{label}</label>
        <output htmlFor={id} className="font-mono text-xs tabular-nums text-neutral-500 dark:text-neutral-400">{format(value)}</output>
      </div>
      <input
        id={id}
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(Number(event.target.value))}
        style={{ background: `linear-gradient(to right, var(--color-signal-500) ${percent}%, var(--color-neutral-300) ${percent}%)` }}
        className="mt-2 h-1.5 w-full cursor-pointer appearance-none rounded-full disabled:cursor-not-allowed [&::-webkit-slider-thumb]:h-4 [&::-webkit-slider-thumb]:w-4 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-white [&::-webkit-slider-thumb]:shadow [&::-webkit-slider-thumb]:ring-1 [&::-webkit-slider-thumb]:ring-neutral-900/20"
      />
    </div>
  );
}
