import { create } from 'zustand';
import { Agent, Thread, MessageEnvelope } from '@/types';

interface AppState {
  agents: Agent[];
  threads: Thread[];
  messages: MessageEnvelope[];
  setAgents: (agents: Agent[]) => void;
  setThreads: (threads: Thread[]) => void;
  setMessages: (messages: MessageEnvelope[]) => void;
  addMessage: (message: MessageEnvelope) => void;
  updateAgent: (agent: Partial<Agent> & { id: string }) => void;
  updateThread: (thread: Partial<Thread> & { id: string }) => void;
}

export const useStore = create<AppState>((set) => ({
  agents: [],
  threads: [],
  messages: [],
  setAgents: (agents) => set({ agents }),
  setThreads: (threads) => set({ threads }),
  setMessages: (messages) => set((state) => {
    // Merge and de-duplicate by id
    const existingIds = new Set(state.messages.map(m => m.id).filter(Boolean));
    const newMessages = messages.filter(m => !m.id || !existingIds.has(m.id));
    return { messages: [...newMessages, ...state.messages].slice(0, 1000) };
  }),
  addMessage: (message) => set((state) => {
    // If message has id and already exists, ignore
    if (message.id && state.messages.some(m => m.id === message.id)) {
      return state;
    }
    return { 
      messages: [message, ...state.messages].slice(0, 1000) 
    };
  }),
  updateAgent: (updatedAgent) => set((state) => ({
    agents: state.agents.map((a) => a.id === updatedAgent.id ? { ...a, ...updatedAgent } : a)
  })),
  updateThread: (updatedThread) => set((state) => ({
    threads: state.threads.map((t) => t.id === updatedThread.id ? { ...t, ...updatedThread } : t)
  })),
}));
