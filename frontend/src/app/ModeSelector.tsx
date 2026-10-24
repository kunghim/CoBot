import { cx, Icons, SegmentedControl } from '../components/ui';
import type { AppMode } from './mode';

export const modeIcons = { chat: Icons.thinking, work: Icons.team };

/** Reserve the end of the sidebar for the mode capsule; its toggle starts at the canvas edge. */
export function modeHeaderClass(listOpen: boolean) {
  return cx('flex [&_[data-testid=chrome-wordmark]]:w-[calc(min(16rem,40vw)-3.875rem)]', !listOpen && '[&_[data-testid=chrome-wordmark]]:hidden [&_[role=radiogroup]]:hidden');
}

export function ModeSelector({ value, onChange, label, chatLabel, workLabel }: { value: AppMode; onChange: (value: AppMode) => void; label: string; chatLabel: string; workLabel: string }) {
  const optionLabel = (text: string) => <span title={text} className="absolute inset-0 rounded-full"><span className="sr-only">{text}</span></span>;
  return <div className="flex [&>[role=radiogroup]]:h-6 [&>[role=radiogroup]]:gap-0.5 [&>[role=radiogroup]]:rounded-full [&>[role=radiogroup]]:p-0.5 [&_[role=radio]]:relative [&_[role=radio]]:h-5 [&_[role=radio]]:w-6 [&_[role=radio]]:justify-center [&_[role=radio]]:gap-0 [&_[role=radio]]:rounded-full [&_[role=radio]]:px-0">
    <SegmentedControl label={label} value={value} options={[
      { value: 'chat', label: optionLabel(chatLabel), icon: modeIcons.chat },
      { value: 'work', label: optionLabel(workLabel), icon: modeIcons.work },
    ]} onChange={onChange} />
  </div>;
}
