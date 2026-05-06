'use client';

import { useEffect } from 'react';
import { wsClient } from '@/lib/websocket/client';
import { useStore } from '@/store/useStore';
import { MessageEnvelope } from '@/types';

export default function WebSocketHandler() {
  const addMessage = useStore((state) => state.addMessage);
  const updateAgent = useStore((state) => state.updateAgent);
  const updateThread = useStore((state) => state.updateThread);

  useEffect(() => {
    if (!wsClient) return;

    wsClient.connect();

    const unsubscribe = wsClient.onMessage((message: MessageEnvelope) => {
      // Add message to board
      addMessage(message);

      // Special handling based on message type
      switch (message.type) {
        case 'status':
          updateAgent({
            id: message.from,
            status: message.payload.state === 'running' ? 'busy' : 'online'
          });
          break;
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
      wsClient.disconnect();
    };
  }, [addMessage, updateAgent, updateThread]);

  return null;
}
