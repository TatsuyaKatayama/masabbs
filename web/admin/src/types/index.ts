export type AgentRole = 'manager' | 'worker' | 'observer' | 'admin';
export type AgentStatus = 'online' | 'offline' | 'busy';

export interface Agent {
  id: string;
  name: string;
  role: AgentRole;
  mission: string;
  tools: Record<string, unknown>[];
  capabilities: string[];
  status: AgentStatus;
  team_id?: string;
  ui_pos_x: number;
  ui_pos_y: number;
  created_at: string;
  updated_at: string;
}

export interface AgentRelation {
  id: string;
  team_id: string;
  source_id: string;
  target_id: string;
  source_handle?: string;
  target_handle?: string;
  relation_type: 'boss' | 'coworker';
  relation_category: 'vertical' | 'horizontal';
}

export interface Team {
  id: string;
  name: string;
  description: string;
  mission: string;
  created_at: string;
  updated_at: string;
}

export interface TeamBlueprint {
  team_id: string;
  structure_mermaid: string;
  members: Agent[];
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
  payload: Record<string, unknown>;
}

export interface Task {
  id: string;
  thread_id?: string;
  agent_id: string;
  type: MessageType;
  to_agents?: string[];
  observers?: string[];
  payload: Record<string, unknown>;
  created_at: string;
}
