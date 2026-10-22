import { describe, expect, it } from 'vitest';
import type { Provider } from '../../api/types';
import { memberChoice } from './providerModels';

const provider = (id: string, models: string[], extra: Partial<Provider> = {}): Provider =>
  ({
    id,
    name: id,
    kind: 'official',
    authMode: 'api_key',
    endpoints: [],
    models: models.map((model) => ({ id: model })),
    enabled: true,
    revision: 1,
    createdAt: '',
    updatedAt: '',
    hasCredential: true,
    ...extra,
  }) as Provider;

const official = provider('openai', [], { authMode: 'chatgpt' });

describe('the model a member runs with', () => {
  it('follows the member, then the team, then the default model', () => {
    const providers = [official, provider('mimo', ['mimo-pro', 'mimo-flash']), provider('deepseek', ['ds'])];
    const settings = { defaultProvider: 'mimo', defaultModel: 'mimo-pro' };
    expect(memberChoice({ providerId: 'deepseek', model: 'ds' }, { providerId: 'mimo' }, settings, providers)).toEqual({ providerId: 'deepseek', model: 'ds' });
    expect(memberChoice({ providerId: '', model: '' }, { providerId: 'deepseek' }, settings, providers)).toEqual({ providerId: 'deepseek', model: 'ds' });
    expect(memberChoice({ providerId: '', model: '' }, undefined, settings, providers)).toEqual({ providerId: 'mimo', model: 'mimo-pro' });
  });

  // The default model is the default provider's. With that provider off, a
  // member falls back to the ChatGPT account, which must not be asked for
  // another vendor's model.
  it('does not carry the default model to another provider', () => {
    const providers = [official, provider('mimo', ['mimo-pro'], { enabled: false })];
    const settings = { defaultProvider: 'mimo', defaultModel: 'mimo-pro' };
    expect(memberChoice(undefined, undefined, settings, providers, 'gpt-default')).toEqual({ providerId: 'openai', model: 'gpt-default' });
  });

  it('keeps a default with no provider as a model of the engine', () => {
    expect(memberChoice(undefined, undefined, { defaultProvider: '', defaultModel: 'gpt-x' }, [official], 'gpt-default')).toEqual({ providerId: 'openai', model: 'gpt-x' });
  });
});
