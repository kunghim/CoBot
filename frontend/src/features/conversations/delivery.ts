// Pure delivery/run label mapping and message text extraction. Raw HTML is
// never rendered anywhere in the app; React escapes plain text for us.
import type { Message, TextBlock } from '../../api/types';

/** i18n key of the delivery status shown under the last user message. An
 * absent or unrecognized status is honestly reported as "needs
 * verification", never assumed delivered. */
export function deliveryLabelKey(status: string | undefined): string {
  switch (status) {
    case 'waiting_dependency':
    case 'queued':
    case 'dispatching':
    case 'accepted':
    case 'cancelled':
    case 'unknown':
      return `conversation.delivery.${status}`;
    default:
      return 'conversation.delivery.unknown';
  }
}

/** i18n key of an active run's honest status text. */
export function runStatusLabelKey(status: string): string {
  switch (status) {
    case 'starting':
    case 'running':
    case 'waiting_approval':
    case 'waiting_input':
    case 'completed':
    case 'failed':
    case 'interrupted':
      return `conversation.run.${status}`;
    default:
      return 'conversation.run.unknown';
  }
}

/** Joins a message's text blocks for display. The wire value is a JSON array
 * of {type, text}; a string form and malformed payloads degrade to "" instead
 * of throwing. */
export function messageText(message: Pick<Message, 'blocks'>): string {
  const blocks: unknown = message.blocks;
  if (typeof blocks === 'string') {
    try {
      return joinBlocks(JSON.parse(blocks));
    } catch {
      return '';
    }
  }
  return joinBlocks(blocks);
}

function joinBlocks(value: unknown): string {
  if (!Array.isArray(value)) return '';
  const parts: string[] = [];
  for (const entry of value) {
    if (entry && typeof entry === 'object' && 'text' in entry) {
      const text = (entry as TextBlock).text;
      if (typeof text === 'string') parts.push(text);
    }
  }
  return parts.join('\n');
}
