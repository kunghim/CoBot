// Moving through the cards that wait on the user, one at a time: stepping past
// the last one loaded asks for the next page, and only moves if that worked.
// Processing a card shrinks the list, so the index stays inside bounds without
// ever auto-advancing (no autoplay by product rule).
import { useState } from 'react';

/** Clamps a pager index into [0, count-1]; empty lists report 0. */
export function clampPagerIndex(index: number, count: number): number {
  if (!Number.isFinite(index) || count <= 0) return 0;
  return Math.min(Math.max(Math.trunc(index), 0), count - 1);
}

export interface CardPages {
  hasNextPage: boolean;
  fetchNextPage: () => Promise<{ isError: boolean }>;
}

export function useCardPager(count: number, pages: CardPages) {
  const [index, setIndex] = useState(0);
  const current = clampPagerIndex(index, count);
  return {
    current,
    onPrevious: () => setIndex(current - 1),
    onNext: () => {
      if (current < count - 1) setIndex(current + 1);
      else if (pages.hasNextPage) void pages.fetchNextPage().then((result) => { if (!result.isError) setIndex(current + 1); });
    },
  };
}
