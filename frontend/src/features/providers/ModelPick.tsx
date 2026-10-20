// The model picker: a wrapped row of chips the user ticks, each chip
// carrying what the last check said about that model. It is a feature
// component (the add-provider dialog and the edit dialog both draw it),
// built from the shared library — a chip is a Chip, a status is a Badge.
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
  /** Placeholder of the manual field. */
  manual: string;
  /** The button next to the manual field. */
  manualAdd: string;
  /** The badge of a model that answered. */
  ok: string;
  /** The badge of a model that did not answer. */
  failed: string;
  /** What to say when the vendor offered nothing to pick. */
  noModels: string;
  /** The line under the chips. */
  note: string;
}

export interface ModelPickProps {
  /** The models on offer, in the order they are shown. */
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

export default function ModelPick({ models, selected, statuses, onToggle, onAdd, labels }: ModelPickProps) {
  const [draft, setDraft] = useState('');

  const add = () => {
    const id = draft.trim();
    if (!id) return;
    onAdd(id);
    setDraft('');
  };

  return (
    <div>
      <div className="flex items-baseline gap-2">
        <span className="text-sm font-medium text-neutral-800 dark:text-neutral-100">{labels.title}</span>
        <span className="ml-auto text-xs text-neutral-500 dark:text-neutral-400">{labels.count}</span>
      </div>

      {models.length > 0 ? (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {models.map((id) => {
            const status = statuses?.[id];
            const broken = status !== undefined && !status.ok;
            return (
              <Chip
                key={id}
                pressed={selected.includes(id)}
                onClick={() => onToggle(id)}
                mono
                tone={broken ? 'danger' : 'neutral'}
                title={broken ? status?.message : undefined}
                trailing={
                  status ? (
                    <Badge tone={status.ok ? 'success' : 'danger'}>
                      {status.ok ? labels.ok : `${labels.failed} · ${status.message ?? ''}`}
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
        <p className="mt-2 text-xs leading-5 text-neutral-500 dark:text-neutral-400">{labels.noModels}</p>
      )}

      <div className="mt-3 flex items-center gap-2">
        <TextInput
          mono
          size="sm"
          value={draft}
          aria-label={labels.manual}
          placeholder={labels.manual}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key !== 'Enter') return;
            event.preventDefault();
            add();
          }}
          className="max-w-64"
        />
        <Button size="sm" onClick={add} disabled={!draft.trim()}>
          {labels.manualAdd}
        </Button>
      </div>

      <p className="mt-2 text-xs leading-5 text-neutral-500 dark:text-neutral-400">{labels.note}</p>
    </div>
  );
}
