// Tree: nested items that open and close (teams and their members). Arrow
// keys move, Right opens or enters, Left closes or goes to the parent.
import { useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { ChevronRight } from 'lucide-react';
import { cx, interactive } from './foundation';

export interface TreeNode {
  id: string;
  label: ReactNode;
  leading?: ReactNode;
  trailing?: ReactNode;
  children?: TreeNode[];
  /** A directory can be empty or not loaded yet. */
  directory?: boolean;
  disabled?: boolean;
}

export function Tree({ label, nodes, selected, onSelect, defaultExpanded = [], expanded: controlled, onExpandedChange }: { label: string; nodes: TreeNode[]; selected?: string; onSelect: (id: string) => void; defaultExpanded?: string[]; expanded?: string[]; onExpandedChange?: (ids: string[]) => void }) {
  const [localExpanded, setExpanded] = useState<Set<string>>(() => new Set(defaultExpanded));
  const expanded = controlled ? new Set(controlled) : localExpanded;
  const items = useRef(new Map<string, HTMLLIElement>());
  const visible: Array<{ node: TreeNode; parent?: string }> = [];
  const walk = (list: TreeNode[], parent?: string) => {
    for (const node of list) {
      visible.push({ node, parent });
      if (node.children && expanded.has(node.id)) walk(node.children, node.id);
    }
  };
  walk(nodes);
  const [focus, setFocus] = useState(selected ?? nodes[0]?.id);
  const focused = visible.some(({ node }) => node.id === focus) ? focus : visible.some(({ node }) => node.id === selected) ? selected : visible[0]?.node.id;
  const toggle = (id: string, open?: boolean) => {
      const next = new Set(expanded);
      if (open ?? !next.has(id)) next.add(id);
      else next.delete(id);
      if (!controlled) setExpanded(next);
      onExpandedChange?.([...next]);
  };
  const move = (id: string | undefined) => {
    if (!id) return;
    setFocus(id);
    items.current.get(id)?.focus();
  };
  const onKeyDown = (event: KeyboardEvent) => {
    const at = visible.findIndex((entry) => entry.node.id === focused);
    const entry = visible[at];
    if (!entry) return;
    const hasChildren = entry.node.directory || !!entry.node.children?.length;
    if (event.key === 'ArrowDown') move(visible[at + 1]?.node.id);
    else if (event.key === 'ArrowUp') move(visible[at - 1]?.node.id);
    else if (event.key === 'ArrowRight' && hasChildren) {
      if (expanded.has(entry.node.id)) move(entry.node.children?.[0]?.id);
      else toggle(entry.node.id, true);
    } else if (event.key === 'ArrowLeft') {
      if (hasChildren && expanded.has(entry.node.id)) toggle(entry.node.id, false);
      else move(entry.parent);
    } else if (event.key === 'Home') move(visible[0]?.node.id);
    else if (event.key === 'End') move(visible[visible.length - 1]?.node.id);
    else if (event.key === 'Enter' || event.key === ' ') {
      if (entry.node.disabled) return;
      if (hasChildren) toggle(entry.node.id);
      else onSelect(entry.node.id);
    } else return;
    event.preventDefault();
  };

  const render = (list: TreeNode[], depth: number): ReactNode =>
    list.map((node) => {
      const hasChildren = node.directory || !!node.children?.length;
      const open = expanded.has(node.id);
      const isSelected = node.id === selected;
      return (
        <li
          key={node.id}
          ref={(element) => {
            if (element) items.current.set(node.id, element);
            else items.current.delete(node.id);
          }}
          role="treeitem"
          aria-expanded={hasChildren ? open : undefined}
          aria-selected={hasChildren ? undefined : isSelected}
          aria-level={depth + 1}
          aria-disabled={node.disabled || undefined}
          tabIndex={node.id === focused ? 0 : -1}
          onFocus={(event) => {
            if (event.target === event.currentTarget) setFocus(node.id);
          }}
          className="outline-none [&:focus-visible>div]:outline-2 [&:focus-visible>div]:outline-signal-500"
        >
          <div
            onClick={() => !node.disabled && (hasChildren ? toggle(node.id) : onSelect(node.id))}
            style={{ paddingLeft: `${depth * 14 + 6}px` }}
            className={cx(
              'flex cursor-pointer items-center gap-2 rounded-lg py-1.5 pr-2 text-sm transition-colors',
              isSelected ? cx(interactive.selected, 'font-medium') : cx(hasChildren ? 'font-semibold text-neutral-800 dark:text-neutral-100' : 'text-neutral-600 dark:text-neutral-300', interactive.hover),
            )}
          >
            {hasChildren ? <ChevronRight size={14} aria-hidden className={cx('shrink-0 text-neutral-400 transition-transform', open && 'rotate-90')} /> : <span className="w-3.5 shrink-0" />}
            {node.leading}
            <span className="min-w-0 flex-1 truncate">{node.label}</span>
            {node.trailing}
          </div>
          {hasChildren && open && <ul role="group">{render(node.children ?? [], depth + 1)}</ul>}
        </li>
      );
    });

  return (
    <ul role="tree" aria-label={label} onKeyDown={onKeyDown} className="space-y-0.5">
      {render(nodes, 0)}
    </ul>
  );
}
