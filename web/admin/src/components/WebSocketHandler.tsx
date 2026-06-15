'use client';

import { useEffect } from 'react';
import { wsClient } from '@/lib/websocket/client';
import { useStore } from '@/store/useStore';
import { Agent, MessageEnvelope, Thread } from '@/types';
import { readJsonArray } from '@/lib/http/json';

export default function WebSocketHandler() {
  const addMessage = useStore((state) => state.addMessage);
  const updateThread = useStore((state) => state.updateThread);
  const setAgents = useStore((state) => state.setAgents);
  const setThreads = useStore((state) => state.setThreads);
  const setMessages = useStore((state) => state.setMessages);

  useEffect(() => {
    // Fetch initial data
    const apiUrl = process.env.NEXT_PUBLIC_API_URL || '';
    
    fetch(`${apiUrl}/api/v1/agents`)
      .then(readJsonArray<Agent>)
      .then(data => {
        setAgents(data);
      })
      .catch(err => console.error('Failed to fetch agents:', err));

    fetch(`${apiUrl}/api/v1/threads`)
      .then(readJsonArray<Thread>)
      .then(data => {
        setThreads(data);
      })
      .catch(err => console.error('Failed to fetch threads:', err));

    fetch(`${apiUrl}/api/v1/tasks`)
      .then(readJsonArray<MessageEnvelope>)
      .then(data => {
        setMessages(data);
      })
      .catch(err => console.error('Failed to fetch tasks:', err));

    if (!wsClient) return;

    wsClient.connect();

    const unsubscribe = wsClient.onMessage((message: MessageEnvelope) => {
      // Add message to board
      addMessage(message);

      // Special handling based on message type
      switch (message.type) {
        case 'assign':
          if (message.thread_id) {
            updateThread({
              id: message.thread_id,
              status: 'assigned',
              assigned_agent: message.to?.[0]
            });
          }
          break;
        case 'result':
          if (message.thread_id) {
            updateThread({
              id: message.thread_id,
              status: 'done'
            });
          }
          break;
      }
    });

    return () => {
      unsubscribe();
      wsClient?.disconnect();
    };
  }, [addMessage, updateThread, setAgents, setMessages, setThreads]);

  return null;
}
