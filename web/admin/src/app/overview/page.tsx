'use client';

import { useState, useEffect } from 'react';
import { Team, TeamBlueprint } from '@/types';
import Mermaid from '@/components/Mermaid';
import { Code, Map, RefreshCw } from 'lucide-react';

export default function OverviewPage() {
  const [teams, setTeams] = useState<Team[]>([]);
  const [selectedTeamId, setSelectedTeamId] = useState<string>('');
  const [blueprint, setBlueprint] = useState<TeamBlueprint | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetch('/api/v1/teams')
      .then(res => res.json())
      .then(data => {
        setTeams(data);
        if (data.length > 0) {
          setSelectedTeamId(data[0].id);
        }
      })
      .catch(() => setError('Failed to fetch teams'));
  }, []);

  useEffect(() => {
    const loadBlueprint = async () => {
      if (!selectedTeamId) return;
      setLoading(true);
      setError(null);
      try {
        const res = await fetch(`/api/v1/teams/${selectedTeamId}/blueprint`);
        if (!res.ok) throw new Error('Failed to fetch blueprint');
        const data = await res.json();
        setBlueprint(data);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'An error occurred');
      } finally {
        setLoading(false);
      }
    };

    loadBlueprint();
  }, [selectedTeamId]);

  const fetchBlueprint = async (teamId: string) => {
    if (!teamId) return;
    setLoading(true);
    setError(null);
    try {
      const res = await fetch(`/api/v1/teams/${teamId}/blueprint`);
      if (!res.ok) throw new Error('Failed to fetch blueprint');
      const data = await res.json();
      setBlueprint(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'An error occurred');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex flex-col h-full space-y-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-extrabold text-slate-900 tracking-tight">System Overview</h1>
          <p className="mt-2 text-lg text-slate-700">Visualize the multi-agent system architecture.</p>
        </div>
        <div className="flex items-center space-x-4">
          <div className="flex items-center space-x-2">
            <label className="text-sm font-medium text-slate-700">Team:</label>
            <select 
              value={selectedTeamId} 
              onChange={(e) => setSelectedTeamId(e.target.value)}
              className="block w-48 rounded-md border-slate-300 py-2 pl-3 pr-10 text-base focus:border-indigo-500 focus:outline-none focus:ring-indigo-500 sm:text-sm border"
            >
              {teams.map(team => (
                <option key={team.id} value={team.id}>{team.name}</option>
              ))}
            </select>
          </div>
          <button 
            onClick={() => fetchBlueprint(selectedTeamId)}
            className="p-2 text-slate-500 hover:text-indigo-600 transition-colors"
            title="Refresh"
          >
            <RefreshCw className={`h-5 w-5 ${loading ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </header>

      {error && (
        <div className="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded relative">
          {error}
        </div>
      )}

      {blueprint ? (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-8 h-full flex-grow">
          {/* Mermaid Code Section */}
          <div className="flex flex-col space-y-4">
            <div className="flex items-center space-x-2 text-slate-800 font-semibold">
              <Code className="h-5 w-5" />
              <h2>Mermaid Definition</h2>
            </div>
            <div className="flex-grow bg-slate-900 rounded-lg overflow-hidden shadow-lg border border-slate-700">
              <textarea
                readOnly
                className="w-full h-full p-6 font-mono text-sm text-indigo-300 bg-slate-900 focus:outline-none resize-none"
                value={blueprint.structure_mermaid}
              />
            </div>
          </div>

          {/* Preview Section */}
          <div className="flex flex-col space-y-4">
            <div className="flex items-center space-x-2 text-slate-800 font-semibold">
              <Map className="h-5 w-5" />
              <h2>Architecture Preview</h2>
            </div>
            <div className="flex-grow bg-white rounded-lg shadow-sm ring-1 ring-slate-200 overflow-auto p-4 flex items-center justify-center">
              <Mermaid chart={blueprint.structure_mermaid} />
            </div>
          </div>
        </div>
      ) : (
        !loading && !error && (
          <div className="flex-grow flex items-center justify-center text-slate-400 italic">
            Select a team to view its overview map.
          </div>
        )
      )}

      {loading && !blueprint && (
        <div className="flex-grow flex items-center justify-center">
          <RefreshCw className="h-8 w-8 text-indigo-600 animate-spin" />
        </div>
      )}
    </div>
  );
}
