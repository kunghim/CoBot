// The team's members by id, so anything shown inside a team's conversation can
// name a member and show its avatar.
import { createContext } from 'react';
import type { Bot } from '../api/types';

export const TeamMembersContext = createContext<ReadonlyMap<string, Bot>>(new Map());
