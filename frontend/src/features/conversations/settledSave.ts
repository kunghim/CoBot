// settledSave turns a stream of changes (dragging a slider across steps) into
// as few saves as possible: it waits until the value has stayed put for a
// moment, never runs two saves at once, and after a save finishes sends only
// the newest value if it moved meanwhile. Each save therefore starts from the
// revision the previous one returned, so they cannot conflict with each other.
export interface SettledSave<T> {
  /** A new value; the save waits until the changes settle. */
  set(value: T): void;
  /** Save the newest value now (the user let go). */
  flush(): void;
  /** Stop any waiting save (the view goes away). */
  dispose(): void;
}

export function createSettledSave<T>(save: (value: T) => Promise<unknown>, delayMs = 400): SettledSave<T> {
  let latest: T | undefined;
  let hasLatest = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let running = false;
  let disposed = false;

  const run = () => {
    if (timer !== undefined) {
      clearTimeout(timer);
      timer = undefined;
    }
    if (running || !hasLatest || disposed) return;
    const value = latest as T;
    hasLatest = false;
    running = true;
    void save(value)
      .catch(() => undefined)
      .finally(() => {
        running = false;
        // A value that arrived while saving goes out next, alone.
        if (hasLatest) run();
      });
  };

  return {
    set(value) {
      latest = value;
      hasLatest = true;
      if (timer !== undefined) clearTimeout(timer);
      timer = setTimeout(run, delayMs);
    },
    flush: run,
    dispose() {
      disposed = true;
      if (timer !== undefined) clearTimeout(timer);
    },
  };
}
