export type AgentRole = 'manager' | 'worker' | 'observer' | 'admin';
export type AgentStatus = 'online' | 'offline' | 'busy';

export interface Agent {
  id: string;
  name: string;
  role: AgentRole;
  mission: string;
  tools: any[];
  capabilities: any[];
  status: AgentStatus;
  team_id?: string;
  created_at: string;
  updated_at: string;
}

export interface Team {
  id: string;
  name: string;
  description: string;
  mission: string;
  created_at: string;
  updated_at: string;
}

export type ThreadStatus = 'open' | 'assigned' | 'collecting' | 'processing' | 'done' | 'error';

export interface Thread {
  id: string;
  parent_thread_id?: string;
  created_by_agent: string;
  assigned_agent?: string;
  status: ThreadStatus;
  created_at: string;
  updated_at: string;
}

export type MessageType = 'task' | 'offer' | 'assign' | 'result' | 'status' | 'event' | 'shutdown';

export interface MessageEnvelope {
  id?: string;
  type: MessageType;
  thread_id?: string;
  from: string;
  to?: string[];
  observers?: string[];
  timestamp: number;
  payload: any;
}

export interface Task {
  id: string;
  thread_id?: string;
  agent_id: string;
  type: MessageType;
  to_agents?: string[];
  observers?: string[];
  payload: any;
  created_at: string;
}
