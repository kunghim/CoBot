// Which models a profile can pick, and where a choice is written.
//
// The provider the profile runs with decides the models: a third-party
// gateway brings its own, and the engine's own list belongs to the engine's
// own vendor. The settings page and the composer must answer this the same
// way — they used to each have their own copy, and the composer went on
// listing the engine's models and writing a setting the core ignores.
import type { Provider, ProviderRequest } from '../../api/types';

/** The provider the profile runs with, if it is still there. */
export function providerOf(providers: Provider[] | undefined, providerID: string | undefined): Provider | undefined {
  if (!providerID) return undefined;
  return providers?.find((entry) => entry.id === providerID);
}

/** The third-party gateway the profile runs with, if any. The official
 * provider is the engine's own account and keeps the engine's model list. */
export function gatewayOf(providers: Provider[] | undefined, providerID: string | undefined): Provider | null {
  const provider = providerOf(providers, providerID);
  return provider && provider.authMode !== 'chatgpt' ? provider : null;
}

export interface ModelOption {
  id: string;
  label: string;
  description?: string;
}

/** The models a gateway offers. A model that failed its check says so. */
export function gatewayModels(gateway: Provider, unavailableLabel: string): ModelOption[] {
  return gateway.models.map((model) => ({
    id: model.id,
    label: model.status === 'failed' ? `${model.displayName || model.id} · ${unavailableLabel}` : model.displayName || model.id,
  }));
}

/** The model a turn will really ask for: the gateway's own default, else its
 * first model — the same order the core picks one in. */
export function gatewayModelID(gateway: Provider): string {
  return gateway.defaultModel ?? gateway.models[0]?.id ?? '';
}

/** The write that changes which model a gateway serves. It is the provider
 * that changes, not the profile: the profile's setting is only the fallback
 * for a profile that runs with no provider at all. */
export function gatewayModelRequest(gateway: Provider, modelID: string): ProviderRequest {
  return {
    id: gateway.id,
    name: gateway.name,
    kind: gateway.kind,
    authMode: gateway.authMode,
    endpoints: gateway.endpoints,
    models: gateway.models,
    defaultModel: modelID,
    retries: gateway.retries,
    presetId: gateway.presetId,
    expectedRevision: gateway.revision,
  };
}
