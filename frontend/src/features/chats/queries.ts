import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';

import { isChatRunning } from './state';

export function useChatNavigation() {
  return useQuery({ queryKey: ['chat-navigation'], queryFn: api.chatNavigation });
}
export function useChats(filter: { pinned?: string; archived?: boolean; sort?: string; search?: string; section?: string } = {}) {
  return useInfiniteQuery({ queryKey: ['chats', filter], initialPageParam: '', queryFn: ({ pageParam }) => api.listChats(pageParam, filter), getNextPageParam: (page) => page.nextCursor || undefined, refetchInterval: 10_000 });
}
export function useChat(id: string | undefined) {
  return useInfiniteQuery({ queryKey: ['chat', id], enabled: !!id, initialPageParam: '', queryFn: ({ pageParam }) => api.getChat(id!, pageParam), getNextPageParam: (page) => page.nextCursor || undefined,
    refetchInterval: (query) => query.state.data?.pages[0]?.runs.some((run) => isChatRunning(run.status)) ? 2000 : 10_000,
  });
}
