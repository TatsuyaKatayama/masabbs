import { create } from 'zustand';
import { Agent, Thread, MessageEnvelope } from '@/types';

interface AppState {
  agents: Agent[];
  threads: Thread[];
  messages: MessageEnvelope[];
  selectedTeamId?: string;
  setAgents: (agents: Agent[]) => void;
  setThreads: (threads: Thread[]) => void;
  setMessages: (messages: MessageEnvelope[]) => void;
  setSelectedTeamId: (teamId?: string) => void;
  addMessage: (message: MessageEnvelope) => void;
  updateAgent: (agent: Partial<Agent> & { id: string }) => void;
  updateThread: (thread: Partial<Thread> & { id: string }) => void;
}

function stableStringify(value: unknown): string {
  if (value === null || typeof value !== 'object') return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(',')}]`;

  return `{${Object.entries(value as Record<string, unknown>)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, item]) => `${JSON.stringify(key)}:${stableStringify(item)}`)
    .join(',')}}`;
}

function messageContentKey(message: MessageEnvelope): string {
  return [
    message.type,
    message.thread_id || '',
    message.from,
    stableStringify(message.to || []),
    stableStringify(message.observers || []),
    stableStringify(message.payload || {}),
  ].join('|');
}

function mergeMessages(current: MessageEnvelope[], incoming: MessageEnvelope[]) {
  const byId = new Map<string, MessageEnvelope>();
  const idByContent = new Map<string, string>();
  const noIdByContent = new Map<string, MessageEnvelope>();

  for (const message of [...incoming, ...current]) {
    const contentKey = messageContentKey(message);

    if (message.id) {
      byId.set(message.id, message);
      idByContent.set(contentKey, message.id);
      noIdByContent.delete(contentKey);
      continue;
    }

    if (idByContent.has(contentKey)) continue;
    if (!noIdByContent.has(contentKey)) {
      noIdByContent.set(contentKey, message);
    }
  }

  return [...byId.values(), ...noIdByContent.values()]
    .sort((a, b) => b.timestamp - a.timestamp)
    .slice(0, 1000);
}

export const useStore = create<AppState>((set) => ({
  agents: [],
  threads: [],
  messages: [],
  selectedTeamId: undefined,
  setAgents: (agents) => set({ agents }),
  setThreads: (threads) => set({ threads }),
  setSelectedTeamId: (selectedTeamId) => set({ selectedTeamId }),
  setMessages: (messages) => set((state) => ({ messages: mergeMessages(state.messages, messages) })),
  addMessage: (message) => set((state) => ({ messages: mergeMessages(state.messages, [message]) })),
  updateAgent: (updatedAgent) => set((state) => ({
    agents: state.agents.map((a) => a.id === updatedAgent.id ? { ...a, ...updatedAgent } : a)
  })),
  updateThread: (updatedThread) => set((state) => ({
    threads: state.threads.map((t) => t.id === updatedThread.id ? { ...t, ...updatedThread } : t)
  })),
}));
