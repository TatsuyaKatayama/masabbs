'use client';

import { useEffect, useState } from 'react';
import { Team } from '@/types';
import { useStore } from '@/store/useStore';
import { readJsonArray } from '@/lib/http/json';

export default function TeamSwitcher() {
  const selectedTeamId = useStore((state) => state.selectedTeamId);
  const setSelectedTeamId = useStore((state) => state.setSelectedTeamId);
  const [teams, setTeams] = useState<Team[]>([]);

  useEffect(() => {
    fetch('/api/v1/teams')
      .then(readJsonArray<Team>)
      .then((data) => {
        setTeams(data);
        if (!selectedTeamId && data.length > 0) {
          setSelectedTeamId(data[0].id);
        }
      })
      .catch((err) => console.error('Failed to fetch teams:', err));
  }, [selectedTeamId, setSelectedTeamId]);

  return (
    <div className="flex items-center space-x-2">
      <label className="text-xs font-black uppercase tracking-widest text-slate-500">Team</label>
      <select
        value={selectedTeamId || ''}
        onChange={(event) => setSelectedTeamId(event.target.value || undefined)}
        className="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-bold text-slate-800 shadow-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
      >
        <option value="">All Teams</option>
        {teams.map((team) => (
          <option key={team.id} value={team.id}>
            {team.name}
          </option>
        ))}
      </select>
    </div>
  );
}
