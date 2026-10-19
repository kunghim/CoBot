import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createSettledSave } from './settledSave';

describe('createSettledSave', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('saves a drag across several steps once, with the last value', async () => {
    const save = vi.fn(async (_value: string) => {});
    const saver = createSettledSave(save, 400);
    for (const step of ['low', 'medium', 'high', 'xhigh']) {
      saver.set(step);
      await vi.advanceTimersByTimeAsync(100);
    }
    expect(save).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(400);
    expect(save).toHaveBeenCalledExactlyOnceWith('xhigh');
  });

  it('never overlaps saves and sends only the newest value after one finishes', async () => {
    let finish!: () => void;
    const save = vi.fn((_value: string) => new Promise<void>((resolve) => (finish = resolve)));
    const saver = createSettledSave(save, 400);
    saver.set('low');
    saver.flush();
    expect(save).toHaveBeenCalledTimes(1);
    // Moves while the first save is still running.
    saver.set('medium');
    saver.flush();
    saver.set('high');
    saver.flush();
    expect(save).toHaveBeenCalledTimes(1);
    finish();
    await vi.advanceTimersByTimeAsync(0);
    expect(save).toHaveBeenCalledTimes(2);
    expect(save).toHaveBeenLastCalledWith('high');
  });

  it('keeps going after a failed save', async () => {
    const save = vi.fn().mockRejectedValueOnce(new Error('conflict')).mockResolvedValue(undefined);
    const saver = createSettledSave(save, 400);
    saver.set('low');
    saver.flush();
    await vi.advanceTimersByTimeAsync(0);
    saver.set('high');
    saver.flush();
    await vi.advanceTimersByTimeAsync(0);
    expect(save).toHaveBeenCalledTimes(2);
    expect(save).toHaveBeenLastCalledWith('high');
  });

  it('drops a waiting save once disposed', async () => {
    const save = vi.fn(async (_value: string) => {});
    const saver = createSettledSave(save, 400);
    saver.set('low');
    saver.dispose();
    await vi.advanceTimersByTimeAsync(1000);
    expect(save).not.toHaveBeenCalled();
  });
});
