// Which models a member can pick, and which one a member runs with.
//
// Every enabled provider is on offer at once: the ChatGPT account brings the
// engine's own list once it is signed in, and every other provider brings
// the models the user kept. A member's own choice wins; a member that chose
// nothing follows the default model the settings name. The settings page and
// the composer must answer this the same way, so the rule lives here once,
// and it follows the core's own order (internal/app/model_selection.go).
import type { Bot, EngineModel, Provider, Settings, Team } from '../../api/types';

/** The model of one provider, as a picker offers it. */
export interface ProviderModelOption {
  /** Stable key for the pair; model ids may themselves contain slashes. */
  key: string;
  providerId: string;
  model: string;
  label: string;
  /** The provider's name, which the picker groups the models under. */
  group: string;
  /** A model that failed its last check is shown but cannot be chosen. */
  unavailable?: boolean;
}

/** The pair a member runs with; an empty provider is the engine's own
 * account with nothing configured. */
export interface ModelChoice {
  providerId: string;
  model: string;
}

export const choiceKey = (providerId: string, model: string) => `${providerId}\u0001${model}`;

/** The ChatGPT account: the engine's own login, with no endpoint or key. */
export const isOfficial = (provider: Provider) => provider.authMode === 'chatgpt';

const engineModelID = (model: EngineModel) => model.slug ?? model.id ?? '';

/** The models every enabled provider offers, provider by provider. */
export function selectableModels(
  providers: Provider[] | undefined,
  engineModels: EngineModel[] | undefined,
  engineSignedIn: boolean,
  providerName: (provider: Provider) => string,
): ProviderModelOption[] {
  const out: ProviderModelOption[] = [];
  for (const provider of providers ?? []) {
    if (!provider.enabled) continue;
    const group = providerName(provider);
    if (isOfficial(provider)) {
      // The account's models are the engine's own, and only a signed-in
      // account can run them.
      if (!engineSignedIn) continue;
      for (const entry of engineModels ?? []) {
        const id = engineModelID(entry);
        if (id) out.push({ key: choiceKey(provider.id, id), providerId: provider.id, model: id, label: entry.displayName ?? id, group });
      }
      continue;
    }
    for (const entry of provider.models) {
      out.push({
        key: choiceKey(provider.id, entry.id),
        providerId: provider.id,
        model: entry.id,
        label: entry.displayName || entry.id,
        group,
        unavailable: entry.status === 'failed',
      });
    }
  }
  return out;
}

/** The model a provider serves for a requested id: the requested one when
 * it keeps it, else its own default, else its first usable model. */
function modelOf(provider: Provider, requested: string, fallback: string, engineDefault: string): string {
  if (isOfficial(provider)) return requested || provider.defaultModel || fallback || engineDefault;
  const has = (id: string) => provider.models.some((entry) => entry.id === id);
  if (requested && has(requested)) return requested;
  if (provider.defaultModel && has(provider.defaultModel)) return provider.defaultModel;
  return provider.models.find((entry) => entry.status !== 'failed')?.id ?? provider.models[0]?.id ?? '';
}

/** The model a member runs with: its own choice, then its team's provider,
 * then the default model, then the first enabled provider. A disabled or
 * removed provider is skipped wherever it is named. On the ChatGPT account
 * with no model named, the engine runs its own default, which engineDefault
 * names so the picker can show it. */
export function memberChoice(
  bot: Pick<Bot, 'providerId' | 'model'> | undefined,
  team: Pick<Team, 'providerId'> | undefined,
  settings: Pick<Settings, 'defaultProvider' | 'defaultModel'> | undefined,
  providers: Provider[] | undefined,
  engineDefault = '',
  officialSignedIn = true,
): ModelChoice {
  const fallback = settings?.defaultModel ?? '';
  const candidates: ModelChoice[] = [];
  if (bot?.providerId) candidates.push({ providerId: bot.providerId, model: bot.model ?? '' });
  if (team?.providerId) candidates.push({ providerId: team.providerId, model: '' });
  if (settings?.defaultProvider) candidates.push({ providerId: settings.defaultProvider, model: fallback });
  for (const candidate of candidates) {
    const provider = providers?.find((entry) => entry.id === candidate.providerId);
    if (provider?.enabled) return { providerId: provider.id, model: modelOf(provider, candidate.model, fallback, engineDefault) };
  }
  // With nothing named, the first enabled provider that can run a turn:
  // the ChatGPT account only once it is signed in.
  const first = providers?.find((entry) => entry.enabled && (!isOfficial(entry) || officialSignedIn));
  if (first) return { providerId: first.id, model: modelOf(first, '', fallback, engineDefault) };
  return { providerId: '', model: fallback || engineDefault };
}

/** The default model the settings name, as a picker shows it. */
export function defaultChoice(settings: Pick<Settings, 'defaultProvider' | 'defaultModel'> | undefined, providers: Provider[] | undefined, engineDefault = '', officialSignedIn = true): ModelChoice {
  return memberChoice(undefined, undefined, settings, providers, engineDefault, officialSignedIn);
}

/** The model the engine runs when it is asked for none: the one it marks as
 * its default, else the first it lists. */
export function engineDefaultModel(models: EngineModel[] | undefined): string {
  const entry = models?.find((model) => model.isDefault) ?? models?.[0];
  return entry ? engineModelID(entry) : '';
}
