export const isChatRunning = (status: string) => ['queued', 'starting', 'running', 'waiting_approval', 'waiting_input'].includes(status);
export const isChatBlocked = (status: string) => isChatRunning(status) || status === 'unknown';
