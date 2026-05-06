import { MessageEnvelope } from '@/types';

type MessageCallback = (message: MessageEnvelope) => void;

class WebSocketClient {
  private socket: WebSocket | null = null;
  private url: string;
  private agentId: string;
  private callbacks: MessageCallback[] = [];
  private reconnectTimeout: NodeJS.Timeout | null = null;

  constructor(url: string, agentId: string) {
    this.url = url;
    this.agentId = agentId;
  }

  connect() {
    if (this.socket) {
      this.socket.close();
    }

    const wsUrl = new URL(this.url);
    wsUrl.searchParams.set('agent_id', this.agentId);

    this.socket = new WebSocket(wsUrl.toString());

    this.socket.onopen = () => {
      console.log('WebSocket connected');
      if (this.reconnectTimeout) {
        clearTimeout(this.reconnectTimeout);
        this.reconnectTimeout = null;
      }
    };

    this.socket.onmessage = (event) => {
      try {
        const message: MessageEnvelope = JSON.parse(event.data);
        this.callbacks.forEach((cb) => cb(message));
      } catch (err) {
        console.error('Failed to parse WebSocket message:', err);
      }
    };

    this.socket.onclose = () => {
      console.log('WebSocket disconnected, retrying in 3 seconds...');
      this.reconnectTimeout = setTimeout(() => this.connect(), 3000);
    };

    this.socket.onerror = (error) => {
      console.error('WebSocket error:', error);
      this.socket?.close();
    };
  }

  onMessage(callback: MessageCallback) {
    this.callbacks.push(callback);
    return () => {
      this.callbacks = this.callbacks.filter((cb) => cb !== callback);
    };
  }

  disconnect() {
    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
    }
    this.socket?.close();
    this.socket = null;
  }
}

export const wsClient = typeof window !== 'undefined' 
  ? new WebSocketClient(process.env.NEXT_PUBLIC_WS_URL || 'ws://localhost:8080/ws', 'admin-ui')
  : null;
