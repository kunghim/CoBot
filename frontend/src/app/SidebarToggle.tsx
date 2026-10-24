import { IconButton } from '../components/ui';

/** A narrow panel outline keeps the title-bar action quieter than the mode selector. */
export function SidebarToggle({ open, onToggle, openLabel, closeLabel }: { open: boolean; onToggle: () => void; openLabel: string; closeLabel: string }) {
  const icon = () => <svg width="16" height="20" viewBox="0 0 16 20" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <rect x="1.5" y="2" width="13" height="16" rx="2.5" />
    <path d="M6 2v16" opacity="0.55" />
    <path d={open ? 'M11 8l-2 2 2 2' : 'M9 8l2 2-2 2'} />
  </svg>;
  // The button moves away from the pointer on toggle; fading its old background leaves a flash at the new position.
  return <span className="inline-flex [&>button]:transition-[color] [&>button]:h-9 [&>button]:text-neutral-400 [&>button:hover]:text-neutral-600 dark:[&>button]:text-neutral-500 dark:[&>button:hover]:text-neutral-300">
    <IconButton size="sm" icon={icon} label={open ? closeLabel : openLabel} aria-expanded={open} onClick={onToggle} />
  </span>;
}
