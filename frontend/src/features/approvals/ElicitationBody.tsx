// ElicitationBody shows what an MCP server asks of the user and, for a form,
// collects the answers: text and numbers as inputs, booleans as switches,
// enums as toggles. A URL mode offers the address instead, opened in the
// team's browser when there is one.
import { useTranslation } from 'react-i18next';
import { ExternalLink } from 'lucide-react';
import { Icons, Switch, TextInput, cx, useToast } from '../../components/ui';
import { useOpenBrowserTab } from '../../api/queries';
import { apiErrorMessage } from '../../i18n/errors';
import { useWorkspace } from '../workspace/WorkspaceProvider';

interface FieldSchema {
  type?: string;
  title?: string;
  description?: string;
  default?: unknown;
  minimum?: number;
  maximum?: number;
  enum?: string[];
  enumNames?: string[];
  oneOf?: { const: string; title: string }[];
  anyOf?: { const: string; title: string }[];
  items?: { enum?: string[]; anyOf?: { const: string; title: string }[]; type?: string };
}

interface ElicitationPayload {
  server?: string;
  mode?: string;
  message?: string;
  requestedSchema?: { properties?: Record<string, FieldSchema>; required?: string[] };
  url?: string;
}

export function parseElicitation(summary: string): ElicitationPayload | null {
  try {
    const parsed = JSON.parse(summary) as ElicitationPayload;
    if (parsed && (parsed.mode === 'url' || parsed.mode === 'form') && (parsed.message || parsed.url)) return parsed;
    return null;
  } catch {
    return null;
  }
}

/** The ask of one MCP server, and the form that answers it. The values live
 * with the card, which sends them with the accept decision. */
export default function ElicitationBody({ summary, value, onChange, teamId }: { summary: string; value: Record<string, unknown>; onChange: (next: Record<string, unknown>) => void; teamId?: string }) {
  const { t } = useTranslation();
  const payload = parseElicitation(summary);
  if (!payload) return null;
  const fields = Object.entries(payload.requestedSchema?.properties ?? {});
  return (
    <div className="space-y-2" data-testid="elicitation-body">
      <p className="whitespace-pre-wrap text-sm text-neutral-900 dark:text-neutral-50">{payload.message}</p>
      {payload.server && <p className="text-2xs text-neutral-500 dark:text-neutral-400">{t('approvals.elicitation.server', { server: payload.server })}</p>}
      {payload.mode === 'url' && payload.url ? <UrlOpen url={payload.url} teamId={teamId} /> : fields.map(([name, schema]) => <Field key={name} name={name} schema={schema} required={payload.requestedSchema?.required?.includes(name)} value={value[name]} onChange={(next) => onChange({ ...value, [name]: next })} />)}
    </div>
  );
}

function Field({ name, schema, required, value, onChange }: { name: string; schema: FieldSchema; required?: boolean; value: unknown; onChange: (next: unknown) => void }) {
  const label = schema.title || name;
  const current = value ?? schema.default;
  const options = schema.oneOf ?? schema.anyOf;
  const multiOptions = schema.items?.anyOf ?? (schema.items?.enum ? schema.items.enum.map((constant) => ({ const: constant, title: constant })) : undefined);
  const isMulti = schema.type === 'array';
  return (
    <div className="space-y-1">
      <p className="text-xs font-medium text-neutral-600 dark:text-neutral-300">
        {label}
        {required ? <span aria-hidden className="ml-0.5 text-signal-600 dark:text-signal-400">*</span> : null}
        {schema.description && <span className="ml-1.5 font-normal text-neutral-500 dark:text-neutral-400">{schema.description}</span>}
      </p>
      {options ? (
        <div className="flex flex-wrap gap-1.5">
          {options.map((option) => (
            <button
              key={option.const}
              type="button"
              aria-pressed={current === option.const}
              onClick={() => onChange(current === option.const ? undefined : option.const)}
              className={cx(
                'rounded-full border px-3 py-1 text-xs transition-colors',
                current === option.const
                  ? 'border-signal-500 bg-signal-500/10 text-signal-700 dark:text-signal-300'
                  : 'border-neutral-300 text-neutral-600 hover:border-neutral-400 dark:border-neutral-700 dark:text-neutral-300',
              )}
            >
              {option.title}
            </button>
          ))}
        </div>
      ) : isMulti && multiOptions ? (
        <div className="flex flex-wrap gap-1.5">
          {multiOptions.map((option) => {
            const picked = Array.isArray(current) ? (current as string[]) : [];
            const on = picked.includes(option.const);
            return (
              <button
                key={option.const}
                type="button"
                aria-pressed={on}
                onClick={() => onChange(on ? picked.filter((entry) => entry !== option.const) : [...picked, option.const])}
                className={cx(
                  'rounded-full border px-3 py-1 text-xs transition-colors',
                  on
                    ? 'border-signal-500 bg-signal-500/10 text-signal-700 dark:text-signal-300'
                    : 'border-neutral-300 text-neutral-600 hover:border-neutral-400 dark:border-neutral-700 dark:text-neutral-300',
                )}
              >
                {option.title}
              </button>
            );
          })}
        </div>
      ) : schema.type === 'boolean' ? (
        <Switch checked={current === true} onChange={onChange} label={label} hideLabel />
      ) : (
        <TextInput
          mono={schema.type === 'number' || schema.type === 'integer'}
          type={schema.type === 'number' || schema.type === 'integer' ? 'number' : 'text'}
          value={current === undefined || current === null ? '' : String(current)}
          min={schema.minimum}
          max={schema.maximum}
          aria-label={label}
          onChange={(event) => onChange(schema.type === 'number' || schema.type === 'integer' ? (event.target.value === '' ? undefined : Number(event.target.value)) : event.target.value)}
        />
      )}
    </div>
  );
}

/** The address an MCP server wants opened, in the team's browser when the
 * conversation has one, else a new tab. */
function UrlOpen({ url, teamId }: { url: string; teamId?: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const workspace = useWorkspace();
  const openOwn = useOpenBrowserTab(teamId ?? '');
  let host = url;
  try {
    host = new URL(url).host;
  } catch {
    // Keep the raw address; opening it navigates all the same.
  }
  const open = () => {
    if (!teamId) {
      window.open(url, '_blank', 'noopener,noreferrer');
      return;
    }
    openOwn.mutate(url, {
      onSuccess: ({ tab }) => {
        workspace.openTab({ id: `browser:${tab.id}`, kind: 'browser', title: tab.title || host, ref: tab.id }, true);
      },
      onError: (error) => toast.show({ tone: 'warning', title: t('conversation.webLink.openFailed'), description: apiErrorMessage(t, error) }),
    });
  };
  return (
    <div className="flex items-center gap-2">
      <ButtonishOpen onClick={open} label={t('approvals.elicitation.open')} />
      <a href={url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 break-all font-mono text-2xs text-neutral-500 hover:text-neutral-800 dark:text-neutral-400 dark:hover:text-neutral-200">
        {url}
        <ExternalLink size={11} aria-hidden />
      </a>
    </div>
  );
}

function ButtonishOpen({ onClick, label }: { onClick: () => void; label: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex items-center gap-1.5 rounded-full border border-neutral-300 px-3 py-1 text-xs font-medium text-neutral-700 transition-colors hover:border-neutral-400 dark:border-neutral-700 dark:text-neutral-200"
    >
      <Icons.webSearch size={12} aria-hidden />
      {label}
    </button>
  );
}
