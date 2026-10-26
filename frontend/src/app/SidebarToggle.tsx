import { PanelToggle } from '../components/ui';

/** The title-bar toggle is the same pair of icons as every other panel
 * toggle, kept quieter than the mode selector beside it. */
export function SidebarToggle({ open, onToggle, openLabel, closeLabel }: { open: boolean; onToggle: () => void; openLabel: string; closeLabel: string }) {
  // The button moves away from the pointer on toggle; fading its old background leaves a flash at the new position.
  return <span className="inline-flex [&>button]:transition-[color] [&>button]:h-9 [&>button]:text-neutral-400 [&>button:hover]:text-neutral-600 dark:[&>button]:text-neutral-500 dark:[&>button:hover]:text-neutral-300">
    <PanelToggle side="left" open={open} onToggle={onToggle} openLabel={openLabel} closeLabel={closeLabel} />
  </span>;
}
