// The model picker: a search box over a scrolling field of chips the user
// ticks, each chip carrying what the last check said about that model. It
// is a feature component (the add-provider dialog and the edit dialog both
// draw it), built from the shared library — a chip is a Chip, a status is a
// Badge.
//
// A piece of interface that more than one view needs belongs in
// components/ui and is registered in the gallery (frontend/src/gallery);
// what stays here belongs to the model provider feature alone.
import { useState } from 'react';
import type { ProviderProbeResult } from '../../api/types';
import { Badge, Button, Chip, TextInput } from '../../components/ui';

export interface ModelPickLabels {
  /** The heading above the chips, e.g. "可用模型". */
  title: string;
  /** The counter on the right, e.g. "已选 3 / 12". */
  count: string;
  /** Placeholder of the search box. */
  search: string;
  /** The button that keeps a searched id the vendor did not list. */
  addAs: (id: string) => string;
  /** What to say when the search matches nothing. */
  noMatch: string;
  /** The badge of a model that answered. */
  ok: string;
  /** The badge of a model that did not answer. */
  failed: string;
  /** Why a model did not answer, in the reader's language. */
  reason: (status: ProviderProbeResult) => string;
  /** What to say when the vendor offered nothing to pick. */
  noModels: string;
  /** The line under the chips. */
  note: string;
}

export interface ModelPickProps {
  /** The models on offer. */
  models: string[];
  /** The models the user kept. */
  selected: string[];
  /** What the last check said, per model id. */
  statuses?: Record<string, ProviderProbeResult>;
  onToggle: (id: string) => void;
  /** Adds a model the vendor did not list (a hand written id). */
  onAdd: (id: string) => void;
  labels: ModelPickLabels;
}

const byName = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

/**
 * The models to show for a search: those whose id contains the query,
 * ignoring case, sorted by name with numbers compared as numbers, so
 * gpt-5.4 comes before gpt-5.10. A gateway lists hundreds of models in
 * whatever order it keeps them, and only a sorted list can be scanned.
 */
export function visibleModels(models: string[], query: string): string[] {
  const needle = query.trim().toLowerCase();
  const shown = needle ? models.filter((id) => id.toLowerCase().includes(needle)) : [...models];
  return shown.sort(byName.compare);
}

export default function ModelPick({ models, selected, statuses, onToggle, onAdd, labels }: ModelPickProps) {
  const [query, setQuery] = useState('');
  const shown = visibleModels(models, query);
  const typed = query.trim();
  // A vendor's list is not always complete, so an id the search finds
  // nothing for can still be kept as typed.
  const canAdd = typed !== '' && shown.length === 0;

  const add = () => {
    if (!canAdd) return;
    onAdd(typed);
    setQuery('');
  };

  return (
    <div>
      <div className="flex items-baseline gap-2">
        <span className="text-sm font-medium text-neutral-800 dark:text-neutral-100">{labels.title}</span>
        <span className="ml-auto text-xs text-neutral-500 dark:text-neutral-400">{labels.count}</span>
      </div>

      <div className="mt-2 flex items-center gap-2">
        <TextInput
          mono
          size="sm"
          type="search"
          value={query}
          aria-label={labels.search}
          placeholder={labels.search}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={(event) => {
            if (event.key !== 'Enter') return;
            event.preventDefault();
            add();
          }}
        />
        {canAdd && (
          <Button size="sm" onClick={add} className="shrink-0">
            {labels.addAs(typed)}
          </Button>
        )}
      </div>

      {/* The field scrolls inside the dialog rather than growing it. */}
      <div className="mt-2 max-h-60 overflow-y-auto">
        {shown.length > 0 ? (
          <div className="flex flex-wrap gap-1.5">
            {shown.map((id) => {
              const status = statuses?.[id];
              const broken = status !== undefined && !status.ok;
              return (
                <Chip
                  key={id}
                  pressed={selected.includes(id)}
                  onClick={() => onToggle(id)}
                  mono
                  tone={broken ? 'danger' : 'neutral'}
                  title={broken && status ? labels.reason(status) : undefined}
                  trailing={
                    status ? (
                      <Badge tone={status.ok ? 'success' : 'danger'}>
                        {status.ok ? labels.ok : `${labels.failed} · ${labels.reason(status)}`}
                      </Badge>
                    ) : undefined
                  }
                >
                  {id}
                </Chip>
              );
            })}
          </div>
        ) : (
          <p className="text-xs leading-5 text-neutral-500 dark:text-neutral-400">{models.length > 0 ? labels.noMatch : labels.noModels}</p>
        )}
      </div>

      <p className="mt-2 text-xs leading-5 text-neutral-500 dark:text-neutral-400">{labels.note}</p>
    </div>
  );
}
