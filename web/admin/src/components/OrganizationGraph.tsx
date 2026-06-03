'use client';

import React, { useState, useEffect, useCallback } from 'react';
import {
  ReactFlow,
  MiniMap,
  Controls,
  Background,
  useNodesState,
  useEdgesState,
  Connection,
  Edge,
  Node,
  MarkerType,
  Handle,
  Position,
  EdgeLabelRenderer,
  getBezierPath,
  EdgeProps,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';

import { Agent, Team, AgentRelation } from '@/types';

// Custom Node component with 4 bidirectional connection points
interface AgentNodeProps {
  data: {
    label: string;
    role: string;
  };
  selected: boolean;
}

const AgentNode = ({ data, selected }: AgentNodeProps) => {
  const handleStyle = { width: '10px', height: '10px', background: '#3b82f6' };
  return (
    <div className={`px-4 py-3 rounded-lg border shadow-md transition-all ${
      data.role === 'manager' ? 'bg-blue-50 border-blue-500' : 'bg-white border-slate-300'
    } ${selected ? 'ring-2 ring-blue-400' : ''}`}
    style={{ minWidth: '140px', textAlign: 'center', position: 'relative' }}>
      
      {/* Top Handle (Source & Target overlaid) */}
      <Handle type="target" position={Position.Top} id="t-t" style={handleStyle} />
      <Handle type="source" position={Position.Top} id="t-s" style={handleStyle} />
      
      {/* Bottom Handle (Source & Target overlaid) */}
      <Handle type="target" position={Position.Bottom} id="b-t" style={handleStyle} />
      <Handle type="source" position={Position.Bottom} id="b-s" style={handleStyle} />

      {/* Left Handle (Source & Target overlaid) */}
      <Handle type="target" position={Position.Left} id="l-t" style={handleStyle} />
      <Handle type="source" position={Position.Left} id="l-s" style={handleStyle} />

      {/* Right Handle (Source & Target overlaid) */}
      <Handle type="target" position={Position.Right} id="r-t" style={handleStyle} />
      <Handle type="source" position={Position.Right} id="r-s" style={handleStyle} />
      
      <div className="text-xs font-bold text-slate-900 whitespace-pre-wrap">{data.label}</div>
    </div>
  );
};

// Custom Edge component for better label positioning
const OrganizationEdge = ({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  style = {},
  markerEnd,
  markerStart,
  label,
}: EdgeProps) => {
  const [edgePath, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  const isBoss = label === 'boss';

  return (
    <>
      <path
        id={id}
        style={style}
        className="react-flow__edge-path"
        d={edgePath}
        markerEnd={markerEnd}
        markerStart={markerStart}
      />
      <EdgeLabelRenderer>
        {isBoss ? (
          <>
            {/* Label near Source (Boss) */}
            <div
              style={{
                position: 'absolute',
                transform: `translate(-50%, -50%) translate(${sourceX + (targetX - sourceX) * 0.2}px,${sourceY + (targetY - sourceY) * 0.2}px)`,
                fontSize: 10,
                fontWeight: 700,
                color: '#3b82f6',
                background: '#eff6ff',
                padding: '2px 4px',
                borderRadius: '4px',
                border: '1px solid #3b82f6',
                pointerEvents: 'all',
              }}
              className="nodrag nopan"
            >
              Boss
            </div>
            {/* Label near Target (Staff) */}
            <div
              style={{
                position: 'absolute',
                transform: `translate(-50%, -50%) translate(${sourceX + (targetX - sourceX) * 0.8}px,${sourceY + (targetY - sourceY) * 0.8}px)`,
                fontSize: 10,
                fontWeight: 700,
                color: '#64748b',
                background: '#f8fafc',
                padding: '2px 4px',
                borderRadius: '4px',
                border: '1px solid #cbd5e1',
                pointerEvents: 'all',
              }}
              className="nodrag nopan"
            >
              Staff
            </div>
          </>
        ) : (
          /* Coworker label in center */
          <div
            style={{
              position: 'absolute',
              transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)`,
              fontSize: 10,
              fontWeight: 700,
              color: '#10b981',
              background: '#f0fdf4',
              padding: '2px 4px',
              borderRadius: '4px',
              border: '1px solid #10b981',
              pointerEvents: 'all',
            }}
            className="nodrag nopan"
          >
            Coworker
          </div>
        )}
      </EdgeLabelRenderer>
    </>
  );
};

const nodeTypes = {
  agent: AgentNode,
};

const edgeTypes = {
  org: OrganizationEdge,
};

interface OrganizationGraphProps {
  initialTeamId?: string;
}

export default function OrganizationGraph({ initialTeamId }: OrganizationGraphProps) {
  const [teams, setTeams] = useState<Team[]>([]);
  const [selectedTeamId, setSelectedTeamId] = useState<string | undefined>(initialTeamId);
  const [allAgents, setAllAgents] = useState<Agent[]>([]);
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [agentToAdd, setAgentToAdd] = useState<string>('');

  // Fetch teams for selector
  useEffect(() => {
    fetch('/api/v1/teams')
      .then(res => res.json())
      .then(data => {
        setTeams(data);
        if (!selectedTeamId && data.length > 0) {
          setSelectedTeamId(data[0].id);
        }
      });
  }, [selectedTeamId]);

  const fetchGraphData = useCallback(async () => {
    if (!selectedTeamId) return;
    try {
      const [agentsRes, relationsRes] = await Promise.all([
        fetch('/api/v1/agents'),
        fetch(`/api/v1/teams/${selectedTeamId}/relations`)
      ]);
      
      const agentsData: Agent[] = await agentsRes.json();
      const relations: AgentRelation[] = await relationsRes.json();
      
      setAllAgents(agentsData);
      const teamAgents = agentsData.filter(a => a.team_id === selectedTeamId);
      
      // Update nodes while preserving current coordinates
      setNodes((currentNodes) => {
        return teamAgents.map((agent, index) => {
          const existingNode = currentNodes.find(n => n.id === agent.id);
          let initialPos = { x: agent.ui_pos_x, y: agent.ui_pos_y };
          if (initialPos.x === 0 && initialPos.y === 0 && !existingNode) {
            initialPos = { x: 100 + (index % 3) * 250, y: 100 + Math.floor(index / 3) * 150 };
          }

          return {
            id: agent.id,
            type: 'agent',
            position: existingNode ? existingNode.position : initialPos,
            data: { 
              label: `${agent.name}\n(${agent.role})`,
              role: agent.role 
            },
          };
        });
      });

      // Update edges with correct arrows and custom type
      setEdges(relations.map(rel => {
        const isBoss = rel.relation_type === 'boss';
        return {
          id: rel.id,
          type: 'org', // Apply custom edge component
          source: rel.source_id,
          target: rel.target_id,
          sourceHandle: rel.source_handle,
          targetHandle: rel.target_handle,
          label: rel.relation_type,
          animated: isBoss,
          markerEnd: {
            type: MarkerType.ArrowClosed,
            color: isBoss ? '#3b82f6' : '#10b981',
          },
          // Coworker has bidirectional arrow
          markerStart: !isBoss ? {
            type: MarkerType.ArrowClosed,
            color: '#10b981',
          } : undefined,
          style: { stroke: isBoss ? '#3b82f6' : '#10b981', strokeWidth: 2 }
        };
      }));
    } catch (error) {
      console.error('Failed to fetch graph data:', error);
    }
  }, [selectedTeamId, setNodes, setEdges]);

  useEffect(() => {
    const loadData = async () => {
      if (selectedTeamId) {
        await fetchGraphData();
      }
    };
    loadData();
  }, [selectedTeamId, fetchGraphData]);

  const handleAddAgent = async () => {
    if (!selectedTeamId || !agentToAdd) return;
    try {
      const res = await fetch(`/api/v1/agents/${agentToAdd}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ team_id: selectedTeamId }),
      });
      if (res.ok) {
        setAgentToAdd('');
        await fetchGraphData();
      }
    } catch (error) {
      console.error('Failed to add agent to team:', error);
    }
  };

  const onNodeDragStop = useCallback(async (_: unknown, node: Node) => {
    try {
      await fetch(`/api/v1/agents/${node.id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          ui_pos_x: node.position.x,
          ui_pos_y: node.position.y,
        }),
      });
    } catch (error) {
      console.error('Failed to save node position:', error);
    }
  }, []);

  const onConnect = useCallback(
    async (params: Connection) => {
      if (!selectedTeamId || !params.source || !params.target) return;

      const relationType = prompt('Enter relation type (boss, coworker):', 'boss');
      if (!relationType || !['boss', 'coworker'].includes(relationType)) {
        if (relationType) alert('Invalid relation type. Use boss or coworker.');
        return;
      }

      try {
        const res = await fetch('/api/v1/relations', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            team_id: selectedTeamId,
            source_id: params.source,
            target_id: params.target,
            source_handle: params.sourceHandle,
            target_handle: params.targetHandle,
            relation_type: relationType,
          }),
        });

        if (res.ok) {
          await fetchGraphData();
        } else {
          const err = await res.json();
          alert(`Failed to create relation: ${err.error}`);
        }
      } catch (error) {
        console.error('Failed to create relation:', error);
      }
    },
    [selectedTeamId, fetchGraphData]
  );

  const onEdgeClick = useCallback(async (_: unknown, edge: Edge) => {
    if (confirm(`Delete relation "${edge.label}"?`)) {
      try {
        const res = await fetch(`/api/v1/relations/${edge.id}`, {
          method: 'DELETE',
        });
        if (res.ok) {
          setEdges((eds) => eds.filter((e) => e.id !== edge.id));
        }
      } catch (error) {
        console.error('Failed to delete relation:', error);
      }
    }
  }, [setEdges]);

  return (
    <div className="w-full h-full flex flex-col">
      <div className="p-4 bg-white border-b flex items-center justify-between">
        <div className="flex items-center space-x-6">
          <div className="flex items-center space-x-2">
            <label className="text-sm font-medium text-slate-700">Team:</label>
            <select 
              value={selectedTeamId} 
              onChange={(e) => setSelectedTeamId(e.target.value)}
              className="block w-40 rounded-md border-slate-300 py-1.5 text-slate-900 focus:ring-blue-500 sm:text-sm"
            >
              {teams.map(team => (
                <option key={team.id} value={team.id}>{team.name}</option>
              ))}
            </select>
          </div>

          <div className="flex items-center space-x-2 border-l pl-6">
            <label className="text-sm font-medium text-slate-700">Add Agent:</label>
            <select 
              value={agentToAdd} 
              onChange={(e) => setAgentToAdd(e.target.value)}
              className="block w-48 rounded-md border-slate-300 py-1.5 text-slate-900 focus:ring-blue-500 sm:text-sm"
            >
              <option value="">Select an agent...</option>
              {allAgents
                .filter(a => a.team_id !== selectedTeamId)
                .map(agent => (
                  <option key={agent.id} value={agent.id}>{agent.name} ({agent.role})</option>
                ))}
            </select>
            <button
              onClick={handleAddAgent}
              disabled={!agentToAdd}
              className="inline-flex items-center px-3 py-1.5 border border-transparent text-xs font-medium rounded shadow-sm text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:bg-slate-300"
            >
              Add
            </button>
          </div>
        </div>
        <div className="text-[10px] text-slate-400 uppercase tracking-wider font-bold text-right">
          Organization Graph Editor v1.2<br/>
          <span className="text-blue-500">Boss (Blue)</span> | <span className="text-emerald-500">Coworker (Green)</span>
        </div>
      </div>
      
      <div className="flex-grow bg-slate-50">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onConnect={onConnect}
          onNodeDragStop={onNodeDragStop}
          onEdgeClick={onEdgeClick}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          fitView
        >
          <Background color="#cbd5e1" gap={20} />
          <Controls />
          <MiniMap />
        </ReactFlow>
      </div>
    </div>
  );
}
