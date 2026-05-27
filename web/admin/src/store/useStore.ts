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
  setMessages: (messages) => set({ messages }),
  addMessage: (message) => set((state) => ({ 
    messages: [message, ...state.messages].slice(0, 500) // Keep last 500
  })),
  updateAgent: (updatedAgent) => set((state) => ({
    agents: state.agents.map((a) => a.id === updatedAgent.id ? { ...a, ...updatedAgent } : a)
  })),
  updateThread: (updatedThread) => set((state) => ({
    threads: state.threads.map((t) => t.id === updatedThread.id ? { ...t, ...updatedThread } : t)
  })),
}));
