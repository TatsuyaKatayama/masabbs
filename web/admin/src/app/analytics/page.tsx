'use client';

import { useStore } from '@/store/useStore';
import { 
  BarChart3, 
  Users, 
  MessageSquare, 
  Clock, 
  AlertCircle, 
  ThumbsUp, 
  TrendingUp, 
  Layers,
  ArrowRight,
  TrendingDown
} from 'lucide-react';
import { useState, useEffect, useRef, useMemo } from 'react';
import * as d3 from 'd3';

interface KPINetworkNode extends d3.SimulationNodeDatum {
  id: string;
  count: number;
  x?: number;
  y?: number;
  fx?: number | null;
  fy?: number | null;
}

interface KPINetworkLink extends d3.SimulationLinkDatum<KPINetworkNode> {
  source: string | KPINetworkNode;
  target: string | KPINetworkNode;
  value: number;
}

interface KPIData {
  thread_id?: string;
  team_id?: string;
  subthread_count?: number;
  max_depth?: number;
  total_threads?: number;
  total_subthreads?: number;
  max_subthread_depth?: number;
  message_stats: {
    total_messages: number;
    sent_counts: Record<string, number>;
    received_counts: Record<string, number>;
  };
  reply_metrics: {
    average_reply_delay_seconds: number;
    unreplied_count: number;
    unreplied_rate: number;
  };
  reflection_stats: {
    average_score: number;
    by_dimension: Record<string, number>;
  };
  network_data: {
    nodes: { id: string; count: number }[];
    links: { source: string; target: string; value: number }[];
  };
}

export default function AnalyticsPage() {
  const threads = useStore((state) => state.threads);
  const [teams, setTeams] = useState<{ id: string; name: string }[]>([]);
  const [analysisType, setAnalyticsType] = useState<'team' | 'thread'>('team');
  const [selectedTeamId, setSelectedTeamId] = useState<string>('');
  const [selectedThreadId, setSelectedThreadId] = useState<string>('');
  
  const [kpiData, setKpiData] = useState<KPIData | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  const svgRef = useRef<SVGSVGElement>(null);
  const tooltipRef = useRef<HTMLDivElement>(null);

  // Fetch Teams
  useEffect(() => {
    const apiUrl = process.env.NEXT_PUBLIC_API_URL || '';
    fetch(`${apiUrl}/api/v1/teams`)
      .then((res) => res.json())
      .then((data) => {
        if (Array.isArray(data)) {
          setTeams(data);
          if (data.length > 0) {
            setSelectedTeamId(data[0].id);
          }
        }
      })
      .catch((err) => console.error('Failed to fetch teams:', err));
  }, []);

  // Set default selected thread
  useEffect(() => {
    if (threads.length > 0 && !selectedThreadId) {
      setSelectedThreadId(threads[0].id);
    }
  }, [threads, selectedThreadId]);

  // Fetch KPI data
  useEffect(() => {
    const apiUrl = process.env.NEXT_PUBLIC_API_URL || '';
    let url = '';

    if (analysisType === 'team' && selectedTeamId) {
      url = `${apiUrl}/api/v1/teams/${selectedTeamId}/kpi`;
    } else if (analysisType === 'thread' && selectedThreadId) {
      url = `${apiUrl}/api/v1/threads/${selectedThreadId}/kpi`;
    } else {
      return;
    }

    setIsLoading(true);
    setError(null);
    fetch(url)
      .then(async (res) => {
        if (!res.ok) {
          const body = await res.json().catch(() => ({}));
          throw new Error(body.error || `HTTP error ${res.status}`);
        }
        return res.json();
      })
      .then((data) => {
        setKpiData(data);
      })
      .catch((err) => {
        console.error('Failed to fetch KPI:', err);
        setError(err instanceof Error ? err.message : 'Failed to fetch analytics data');
        setKpiData(null);
      })
      .finally(() => {
        setIsLoading(false);
      });
  }, [analysisType, selectedTeamId, selectedThreadId]);

  // Render D3.js Network Diagram
  useEffect(() => {
    if (!svgRef.current || !kpiData || !kpiData.network_data) return;

    const network = kpiData.network_data;
    // Deep copy nodes and links to prevent mutating state
    const nodes: KPINetworkNode[] = network.nodes.map(n => ({ ...n }));
    const links: KPINetworkLink[] = network.links.map(l => ({ ...l }));

    const svgElement = d3.select(svgRef.current);
    svgElement.selectAll('*').remove();

    const width = 800;
    const height = 500;

    // Arrow markers
    svgElement.append('defs').append('marker')
      .attr('id', 'arrow')
      .attr('viewBox', '0 -5 10 10')
      .attr('refX', 22)
      .attr('refY', 0)
      .attr('markerWidth', 6)
      .attr('markerHeight', 6)
      .attr('orient', 'auto')
      .append('path')
      .attr('d', 'M0,-5L10,0L0,5')
      .attr('fill', '#94a3b8');

    const simulation = d3.forceSimulation<KPINetworkNode>(nodes)
      .force('link', d3.forceLink<KPINetworkNode, KPINetworkLink>(links).id(d => d.id).distance(150))
      .force('charge', d3.forceManyBody().strength(-900))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide().radius(60));

    const link = svgElement.append('g')
      .selectAll('path')
      .data(links)
      .enter().append('path')
      .attr('stroke', '#475569')
      .attr('stroke-opacity', 0.5)
      .attr('fill', 'none')
      .attr('stroke-width', d => Math.max(2, Math.sqrt(d.value) * 3))
      .attr('marker-end', 'url(#arrow)');

    const node = svgElement.append('g')
      .selectAll('circle')
      .data(nodes)
      .enter().append('circle')
      .attr('r', d => Math.max(15, Math.sqrt(d.count) * 8 + 10))
      .attr('fill', d => {
        const idLower = d.id.toLowerCase();
        if (idLower.includes('manager') || idLower.includes('leader')) return '#6366f1'; // Indigo for leaders
        if (idLower.includes('chef')) return '#ec4899'; // Pink for chefs
        if (idLower.includes('reviewer')) return '#f43f5e'; // Red for reviewers
        if (idLower.includes('foamer') || idLower.includes('omc')) return '#10b981'; // Green for specific tools
        return '#475569'; // Slate for workers
      })
      .attr('stroke', '#ffffff')
      .attr('stroke-width', 2)
      .attr('cursor', 'grab')
      .call(d3.drag<SVGCircleElement, KPINetworkNode>()
        .on('start', (event, d) => {
          if (!event.active) simulation.alphaTarget(0.3).restart();
          d.fx = d.x;
          d.fy = d.y;
        })
        .on('drag', (event, d) => {
          d.fx = event.x;
          d.fy = event.y;
        })
        .on('end', (event, d) => {
          if (!event.active) simulation.alphaTarget(0);
          d.fx = null;
          d.fy = null;
        }))
      .on('mouseover', (event, d) => {
        if (tooltipRef.current) {
          const tooltip = d3.select(tooltipRef.current);
          tooltip.style('opacity', 1)
            .html(`
              <div class="font-bold text-indigo-400 text-sm mb-1">${d.id}</div>
              <div class="text-xs text-slate-300">Sent Messages: <span class="font-black text-white">${d.count}</span></div>
            `)
            .style('left', (event.clientX + 15) + 'px')
            .style('top', (event.clientY - 15) + 'px');
        }
        d3.select(event.currentTarget)
          .attr('stroke', '#818cf8')
          .attr('stroke-width', 4)
          .style('filter', 'brightness(1.2)');
      })
      .on('mousemove', (event) => {
        if (tooltipRef.current) {
          d3.select(tooltipRef.current)
            .style('left', (event.clientX + 15) + 'px')
            .style('top', (event.clientY - 15) + 'px');
        }
      })
      .on('mouseout', (event) => {
        if (tooltipRef.current) {
          d3.select(tooltipRef.current).style('opacity', 0);
        }
        d3.select(event.currentTarget)
          .attr('stroke', '#ffffff')
          .attr('stroke-width', 2)
          .style('filter', 'brightness(1)');
      });

    const label = svgElement.append('g')
      .selectAll('text')
      .data(nodes)
      .enter().append('text')
      .attr('text-anchor', 'middle')
      .attr('fill', '#f8fafc')
      .attr('font-size', '12px')
      .attr('font-weight', 'bold')
      .attr('pointer-events', 'none')
      .style('text-shadow', '0 2px 4px rgba(0,0,0,0.9)')
      .text(d => d.id);

    simulation.on('tick', () => {
      link.attr('d', d => {
        const sourceNode = d.source as KPINetworkNode;
        const targetNode = d.target as KPINetworkNode;
        const dx = targetNode.x! - sourceNode.x!;
        const dy = targetNode.y! - sourceNode.y!;
        const dr = Math.sqrt(dx * dx + dy * dy);
        return `M${sourceNode.x!},${sourceNode.y!}A${dr},${dr} 0 0,1 ${targetNode.x!},${targetNode.y!}`;
      });

      node.attr('cx', d => d.x!).attr('cy', d => d.y!);
      label.attr('x', d => d.x!).attr('y', d => d.y! - Math.max(18, Math.sqrt(d.count) * 8 + 14));
    });

  }, [kpiData]);

  // Format reply delay
  const formatDelay = (seconds: number) => {
    if (seconds === 0) return '0s';
    if (seconds < 60) return `${Math.round(seconds)}s`;
    const mins = Math.floor(seconds / 60);
    const secs = Math.round(seconds % 60);
    return `${mins}m ${secs}s`;
  };

  return (
    <div className="flex-1 overflow-y-auto bg-slate-950 p-8 text-slate-100">
      <div id="tooltip" ref={tooltipRef} className="pointer-events-none absolute z-50 rounded-lg border border-slate-700 bg-slate-900/95 p-3 shadow-xl opacity-0 transition-opacity duration-200" />

      {/* Header */}
      <div className="mb-8 flex flex-col justify-between md:flex-row md:items-center">
        <div>
          <h1 className="text-3xl font-black tracking-tight text-white flex items-center">
            <BarChart3 className="mr-3 h-8 w-8 text-indigo-500" />
            KPI Analytics Dashboard
          </h1>
          <p className="mt-1 text-sm text-slate-400">
            Analyze agent flows, response latencies, and mutual reflection evaluations.
          </p>
        </div>

        {/* Selection Control */}
        <div className="mt-4 flex flex-wrap items-center gap-3 md:mt-0">
          <div className="flex bg-slate-900 p-1 rounded-lg border border-slate-800">
            <button
              onClick={() => setAnalyticsType('team')}
              className={`rounded-md px-3 py-1.5 text-xs font-bold transition-all ${
                analysisType === 'team'
                  ? 'bg-indigo-600 text-white shadow-sm'
                  : 'text-slate-400 hover:text-white'
              }`}
            >
              Team KPI
            </button>
            <button
              onClick={() => setAnalyticsType('thread')}
              className={`rounded-md px-3 py-1.5 text-xs font-bold transition-all ${
                analysisType === 'thread'
                  ? 'bg-indigo-600 text-white shadow-sm'
                  : 'text-slate-400 hover:text-white'
              }`}
            >
              Thread KPI
            </button>
          </div>

          {analysisType === 'team' ? (
            <select
              value={selectedTeamId}
              onChange={(e) => setSelectedTeamId(e.target.value)}
              className="rounded-lg border border-slate-800 bg-slate-900 px-3 py-2 text-sm font-bold text-white shadow-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
            >
              <option value="" disabled>Select Team</option>
              {teams.map((t) => (
                <option key={t.id} value={t.id}>{t.name}</option>
              ))}
            </select>
          ) : (
            <select
              value={selectedThreadId}
              onChange={(e) => setSelectedThreadId(e.target.value)}
              className="rounded-lg border border-slate-800 bg-slate-900 px-3 py-2 text-sm font-bold text-white shadow-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 max-w-[240px] truncate"
            >
              <option value="" disabled>Select Thread</option>
              {threads.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.id} - {t.created_by_agent}
                </option>
              ))}
            </select>
          )}
        </div>
      </div>

      {isLoading && (
        <div className="flex h-96 items-center justify-center">
          <div className="h-12 w-12 animate-spin rounded-full border-4 border-indigo-500 border-t-transparent" />
        </div>
      )}

      {error && !isLoading && (
        <div className="rounded-xl border border-rose-900/50 bg-rose-950/20 p-6 text-center text-rose-300">
          <AlertCircle className="mx-auto mb-3 h-12 w-12 text-rose-500" />
          <h3 className="text-lg font-bold">Failed to Load Analytics</h3>
          <p className="mt-1 text-sm text-rose-400/80">{error}</p>
        </div>
      )}

      {!kpiData && !isLoading && !error && (
        <div className="rounded-xl border border-slate-800 bg-slate-900/20 p-12 text-center text-slate-400">
          <AlertCircle className="mx-auto mb-3 h-12 w-12 text-slate-600" />
          <h3 className="text-lg font-bold">No Data Available</h3>
          <p className="mt-1 text-sm">Please select another team or thread.</p>
        </div>
      )}

      {kpiData && !isLoading && (
        <div className="space-y-6">
          {/* KPI Metrics Cards */}
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            
            {/* Card 1: Total Messages */}
            <div className="rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-sm">
              <div className="flex items-center justify-between">
                <span className="text-sm font-bold text-slate-400 uppercase tracking-wider">Total Messages</span>
                <MessageSquare className="h-6 w-6 text-indigo-500" />
              </div>
              <div className="mt-3 flex items-baseline">
                <span className="text-3xl font-extrabold text-white">{kpiData.message_stats.total_messages}</span>
                <span className="ml-1 text-sm text-slate-400">sent</span>
              </div>
            </div>

            {/* Card 2: Threads Metrics */}
            <div className="rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-sm">
              <div className="flex items-center justify-between">
                <span className="text-sm font-bold text-slate-400 uppercase tracking-wider">
                  {analysisType === 'team' ? 'Total Threads' : 'Subthreads / Depth'}
                </span>
                <Layers className="h-6 w-6 text-pink-500" />
              </div>
              {analysisType === 'team' ? (
                <div className="mt-3 flex items-baseline justify-between">
                  <div>
                    <span className="text-3xl font-extrabold text-white">{kpiData.total_threads}</span>
                    <span className="ml-1 text-xs text-slate-400">roots</span>
                  </div>
                  <div className="text-right">
                    <span className="text-sm font-bold text-slate-300">+{kpiData.total_subthreads}</span>
                    <span className="ml-1 text-xs text-slate-400">subs</span>
                  </div>
                </div>
              ) : (
                <div className="mt-3 flex items-baseline justify-between">
                  <div>
                    <span className="text-3xl font-extrabold text-white">{kpiData.subthread_count}</span>
                    <span className="ml-1 text-xs text-slate-400">subthreads</span>
                  </div>
                  <div className="text-right">
                    <span className="text-sm font-bold text-slate-300">Depth {kpiData.max_depth}</span>
                    <span className="ml-1 text-xs text-slate-400">max</span>
                  </div>
                </div>
              )}
            </div>

            {/* Card 3: Reply Delays */}
            <div className="rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-sm">
              <div className="flex items-center justify-between">
                <span className="text-sm font-bold text-slate-400 uppercase tracking-wider">Avg Reply Delay</span>
                <Clock className="h-6 w-6 text-emerald-500" />
              </div>
              <div className="mt-3 flex items-baseline">
                <span className="text-3xl font-extrabold text-white">
                  {formatDelay(kpiData.reply_metrics.average_reply_delay_seconds)}
                </span>
              </div>
            </div>

            {/* Card 4: Unreplied rate */}
            <div className="rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-sm">
              <div className="flex items-center justify-between">
                <span className="text-sm font-bold text-slate-400 uppercase tracking-wider">Unreplied Rate</span>
                {kpiData.reply_metrics.unreplied_rate > 0.15 ? (
                  <TrendingUp className="h-6 w-6 text-rose-500" />
                ) : (
                  <TrendingDown className="h-6 w-6 text-emerald-500" />
                )}
              </div>
              <div className="mt-3 flex items-baseline justify-between">
                <div>
                  <span className="text-3xl font-extrabold text-white">
                    {(kpiData.reply_metrics.unreplied_rate * 100).toFixed(1)}%
                  </span>
                </div>
                <div className="text-right">
                  <span className="text-sm font-bold text-slate-300">{kpiData.reply_metrics.unreplied_count}</span>
                  <span className="ml-1 text-xs text-slate-400">pending</span>
                </div>
              </div>
            </div>
          </div>

          {/* Grid Layout: Graph & Evaluation */}
          <div className="grid gap-6 lg:grid-cols-3">
            
            {/* Left/Middle Column: Network Diagram (D3.js) */}
            <div className="rounded-2xl border border-slate-800 bg-slate-900 shadow-md lg:col-span-2">
              <div className="border-b border-slate-800 px-6 py-4 flex items-center justify-between">
                <h3 className="text-lg font-bold text-white flex items-center">
                  <Users className="mr-2.5 h-5 w-5 text-indigo-500" />
                  Interaction Flow Map (D3.js)
                </h3>
                <span className="rounded-full bg-slate-800 px-3 py-1 text-xs font-black uppercase text-indigo-400">
                  {kpiData.network_data?.nodes?.length || 0} nodes
                </span>
              </div>
              
              <div className="relative p-6 flex justify-center items-center h-[520px]">
                <div className="absolute top-4 left-4 bg-slate-950/70 p-3 rounded-lg border border-slate-800 font-mono text-[10px] space-y-1 z-10">
                  <div className="font-bold mb-1 border-b border-slate-800 pb-1 uppercase tracking-widest text-slate-400">Legend</div>
                  <div className="flex items-center"><span className="w-2.5 h-2.5 rounded-full bg-[#6366f1] mr-1.5" /> Leader/Manager</div>
                  <div className="flex items-center"><span className="w-2.5 h-2.5 rounded-full bg-[#ec4899] mr-1.5" /> Chef</div>
                  <div className="flex items-center"><span className="w-2.5 h-2.5 rounded-full bg-[#f43f5e] mr-1.5" /> Reviewer</div>
                  <div className="flex items-center"><span className="w-2.5 h-2.5 rounded-full bg-[#10b981] mr-1.5" /> Tool/Foamer</div>
                  <div className="flex items-center"><span className="w-2.5 h-2.5 rounded-full bg-[#475569] mr-1.5" /> Worker</div>
                  <div className="text-slate-500 border-t border-slate-800 pt-1 mt-1 font-sans">• Drag nodes to rearrange</div>
                </div>

                <div className="w-full h-full bg-slate-950 rounded-xl overflow-hidden border border-slate-800/80">
                  <svg ref={svgRef} className="w-full h-full" viewBox="0 0 800 500" preserveAspectRatio="xMidYMid meet" />
                </div>
              </div>
            </div>

            {/* Right Column: Reflection Scores and Rankings */}
            <div className="space-y-6 lg:col-span-1">
              
              {/* Mutual Reflections Score */}
              <div className="rounded-2xl border border-slate-800 bg-slate-900 p-6 shadow-md">
                <h3 className="text-lg font-bold text-white flex items-center border-b border-slate-800 pb-4 mb-4">
                  <ThumbsUp className="mr-2.5 h-5 w-5 text-emerald-500" />
                  Team Reflections Score
                </h3>
                
                <div className="text-center py-6">
                  <div className="text-5xl font-black text-white">
                    {kpiData.reflection_stats.average_score !== 0 
                      ? (kpiData.reflection_stats.average_score).toFixed(2)
                      : 'N/A'
                    }
                  </div>
                  <p className="mt-1 text-xs text-slate-400 uppercase tracking-widest font-black">
                    Overall Average Score
                  </p>
                </div>

                {/* Dimension breakdown */}
                <div className="space-y-4">
                  <h4 className="text-xs font-black uppercase tracking-widest text-slate-500">Breakdown by Dimension</h4>
                  {Object.keys(kpiData.reflection_stats.by_dimension).length === 0 ? (
                    <div className="text-sm text-slate-500 italic py-4 text-center">No reflection evaluations submitted yet.</div>
                  ) : (
                    Object.entries(kpiData.reflection_stats.by_dimension).map(([dim, score]) => {
                      // Normalize score from -1..1 to 0..100%
                      const percentage = ((score + 1) / 2) * 100;
                      return (
                        <div key={dim} className="space-y-1.5">
                          <div className="flex justify-between text-xs font-bold">
                            <span className="capitalize text-slate-300 font-mono">{dim}</span>
                            <span className={`${score >= 0.5 ? 'text-emerald-400' : score < 0 ? 'text-rose-400' : 'text-slate-400'}`}>
                              {score > 0 ? '+' : ''}{score.toFixed(2)}
                            </span>
                          </div>
                          <div className="h-2 w-full bg-slate-800 rounded-full overflow-hidden">
                            <div 
                              className={`h-full rounded-full transition-all duration-500 ${
                                score >= 0.5 
                                  ? 'bg-emerald-500' 
                                  : score < 0 
                                  ? 'bg-rose-500' 
                                  : 'bg-indigo-500'
                              }`}
                              style={{ width: `${percentage}%` }}
                            />
                          </div>
                        </div>
                      );
                    })
                  )}
                </div>
              </div>

              {/* Message Rankings (Top Senders) */}
              <div className="rounded-2xl border border-slate-800 bg-slate-900 p-6 shadow-md">
                <h3 className="text-lg font-bold text-white flex items-center border-b border-slate-800 pb-4 mb-4">
                  <TrendingUp className="mr-2.5 h-5 w-5 text-indigo-500" />
                  Activity Leaderboard
                </h3>

                <div className="space-y-3">
                  <h4 className="text-xs font-black uppercase tracking-widest text-slate-500 mb-3">Top Message Senders</h4>
                  {Object.keys(kpiData.message_stats.sent_counts).length === 0 ? (
                    <div className="text-sm text-slate-500 italic py-4 text-center">No messages sent in this scope.</div>
                  ) : (
                    Object.entries(kpiData.message_stats.sent_counts)
                      .sort((a, b) => b[1] - a[1])
                      .slice(0, 5)
                      .map(([agentId, count], idx) => (
                        <div key={agentId} className="flex items-center justify-between bg-slate-950 px-4 py-2.5 rounded-lg border border-slate-800/60">
                          <div className="flex items-center">
                            <span className="text-xs font-extrabold text-slate-500 w-5">{idx + 1}</span>
                            <span className="text-sm font-bold text-slate-100 font-mono">{agentId}</span>
                          </div>
                          <span className="text-xs font-black bg-indigo-950 text-indigo-400 px-2.5 py-1 rounded-md border border-indigo-900/50">
                            {count} msgs
                          </span>
                        </div>
                      ))
                  )}
                </div>
              </div>

            </div>

          </div>
        </div>
      )}
    </div>
  );
}
