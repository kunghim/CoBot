import { describe, expect, it } from 'vitest';
import type { Message } from '../../api/types';
import { deliveryLabelKey, messageText } from './delivery';

describe('delivery status labels', () => {
  it('maps every real delivery status onto its own key', () => {
    expect(deliveryLabelKey('queued')).toBe('conversation.delivery.queued');
    expect(deliveryLabelKey('dispatching')).toBe('conversation.delivery.dispatching');
    expect(deliveryLabelKey('accepted')).toBe('conversation.delivery.accepted');
    expect(deliveryLabelKey('cancelled')).toBe('conversation.delivery.cancelled');
    expect(deliveryLabelKey('waiting_dependency')).toBe('conversation.delivery.waiting_dependency');
  });

  it('reports unknown and absent statuses honestly', () => {
    expect(deliveryLabelKey('unknown')).toBe('conversation.delivery.unknown');
    expect(deliveryLabelKey(undefined)).toBe('conversation.delivery.unknown');
    expect(deliveryLabelKey('surprise')).toBe('conversation.delivery.unknown');
  });
});

describe('message text extraction', () => {
  const base = { id: 'm1', teamId: 't1', botId: 'b1', kind: 'user', createdAt: '' } as const;

  it('joins text blocks in order', () => {
    const message = { ...base, blocks: [{ type: 'text', text: 'hello' }, { type: 'text', text: 'world' }] } as Message;
    expect(messageText(message)).toBe('hello\nworld');
  });

  it('tolerates a JSON string form and malformed payloads without throwing', () => {
    expect(messageText({ ...base, blocks: '[{"type":"text","text":"ok"}]' } as unknown as Message)).toBe('ok');
    expect(messageText({ ...base, blocks: 'not-json' } as unknown as Message)).toBe('');
    expect(messageText({ ...base, blocks: [{ type: 'text' }] } as unknown as Message)).toBe('');
    expect(messageText({ ...base, blocks: null } as unknown as Message)).toBe('');
  });
});
