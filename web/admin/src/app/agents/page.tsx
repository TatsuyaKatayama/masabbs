'use client';

import { useStore } from '@/store/useStore';
import { 
  Users, 
  Circle,
  Plus,
  X,
  Shield,
  Target,
  Save,
  Info,
  LayoutGrid,
  Edit2
} from 'lucide-react';
import { useState, useEffect } from 'react';
import { Agent, Team } from '@/types';

export default function AgentsPage() {
  const agents = useStore((state) => state.agents);
  const setAgents = useStore((state) => state.setAgents);
  const [teams, setTeams] = useState<Team[]>([]);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [isDetailModalOpen, setIsDetailModalOpen] = useState(false);
  const [isTeamModalOpen, setIsTeamModalOpen] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null);
  const [selectedTeam, setSelectedTeam] = useState<Team | null>(null);

  const [formData, setFormData] = useState({
    id: '',
    name: '',
    role: 'worker',
    mission: '',
    team_id: '01H0V6P6V6P6V6P6V6P6V6P6V6' // Default Core Team
  });

  const [detailFormData, setDetailFormData] = useState({
    role: 'worker',
    mission: ''
  });

  const [teamFormData, setTeamFormData] = useState({
    mission: ''
  });

  const refreshAgents = async () => {
    const apiUrl = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
    try {
      const res = await fetch(`${apiUrl}/api/v1/agents`);
      const data = await res.json();
      if (Array.isArray(data)) setAgents(data);
    } catch (err) {
      console.error('Failed to refresh agents:', err);
    }
  };

  const refreshTeams = async () => {
    const apiUrl = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
    try {
      const res = await fetch(`${apiUrl}/api/v1/teams`);
      const data = await res.json();
      if (Array.isArray(data)) setTeams(data);
    } catch (err) {
      console.error('Failed to refresh teams:', err);
    }
  };

  useEffect(() => {
    refreshAgents();
    refreshTeams();
  }, []);

  const handleOpenDetails = (agent: Agent) => {
    setSelectedAgent(agent);
    setDetailFormData({
      role: agent.role,
      mission: agent.mission || ''
    });
    setIsDetailModalOpen(true);
  };

  const handleOpenTeamEdit = (team: Team) => {
    setSelectedTeam(team);
    setTeamFormData({
      mission: team.mission || ''
    });
    setIsTeamModalOpen(true);
  };

  const handleUpdateTeam = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedTeam) return;
    
    setIsSubmitting(true);
    setError(null);

    const apiUrl = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
    try {
      const response = await fetch(`${apiUrl}/api/v1/teams/${selectedTeam.id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(teamFormData),
      });

      if (!response.ok) {
        const data = await response.json();
        throw new Error(data.error || 'Failed to update team');
      }

      setIsTeamModalOpen(false);
      refreshTeams();
    } catch (err: any) {
      setError(err.message);
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleUpdateAgent = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedAgent) return;
    
    setIsSubmitting(true);
    setError(null);

    const apiUrl = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
    try {
      const response = await fetch(`${apiUrl}/api/v1/agents/${selectedAgent.id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(detailFormData),
      });

      if (!response.ok) {
        const data = await response.json();
        throw new Error(data.error || 'Failed to update agent');
      }

      setIsDetailModalOpen(false);
      refreshAgents();
    } catch (err: any) {
      setError(err.message);
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSubmitting(true);
    setError(null);

    const apiUrl = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
    try {
      const response = await fetch(`${apiUrl}/api/v1/agents`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(formData),
      });

      if (!response.ok) {
        const data = await response.json();
        throw new Error(data.error || 'Failed to create agent');
      }

      setIsModalOpen(false);
      setFormData({ id: '', name: '', role: 'worker', mission: '', team_id: '01H0V6P6V6P6V6P6V6P6V6P6V6' });
      refreshAgents();
    } catch (err: any) {
      setError(err.message);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="space-y-8">
      <header className="flex justify-between items-center">
        <div>
          <h1 className="text-3xl font-extrabold text-slate-900 tracking-tight">Organization</h1>
          <p className="mt-2 text-lg text-slate-700 font-medium">Manage teams and their autonomous agents.</p>
        </div>
        <button 
          onClick={() => setIsModalOpen(true)}
          className="bg-indigo-600 text-white px-4 py-2 rounded-md hover:bg-indigo-700 transition-colors flex items-center shadow-sm"
        >
          <Plus className="mr-2 h-4 w-4" />
          Add Agent
        </button>
      </header>

      {/* Team Mission Section */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {teams.map(team => (
          <div key={team.id} className="bg-white rounded-xl shadow-sm ring-1 ring-slate-200 overflow-hidden border-l-4 border-indigo-500">
            <div className="px-6 py-4 bg-slate-50 border-b border-slate-200 flex justify-between items-center">
              <div className="flex items-center space-x-2">
                <LayoutGrid className="h-4 w-4 text-indigo-600" />
                <h3 className="font-bold text-slate-900">{team.name} Mission</h3>
              </div>
              <button 
                onClick={() => handleOpenTeamEdit(team)}
                className="text-slate-400 hover:text-indigo-600 transition-colors"
              >
                <Edit2 className="h-4 w-4" />
              </button>
            </div>
            <div className="p-6">
              <p className="text-sm text-slate-600 italic leading-relaxed">
                "{team.mission || 'No overall mission defined for this team yet.'}"
              </p>
            </div>
          </div>
        ))}
      </div>

      <div className="bg-white shadow-sm ring-1 ring-slate-200 rounded-xl overflow-hidden border-b border-slate-300">
        <table className="min-w-full divide-y divide-slate-200">
          <thead className="bg-slate-50">
            <tr>
              <th scope="col" className="px-6 py-4 text-left text-[10px] font-black text-slate-400 uppercase tracking-widest">Agent Info</th>
              <th scope="col" className="px-6 py-4 text-left text-[10px] font-black text-slate-400 uppercase tracking-widest">System Role</th>
              <th scope="col" className="px-6 py-4 text-left text-[10px] font-black text-slate-400 uppercase tracking-widest">Mission</th>
              <th scope="col" className="px-6 py-4 text-left text-[10px] font-black text-slate-400 uppercase tracking-widest">Status</th>
              <th scope="col" className="relative px-6 py-4">
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-slate-100">
            {agents.map((agent) => (
              <tr key={agent.id} className="hover:bg-slate-50/50 transition-colors">
                <td className="px-6 py-5 whitespace-nowrap">
                  <div className="flex items-center">
                    <div className="h-10 w-10 flex-shrink-0 flex items-center justify-center bg-indigo-50 rounded-xl text-indigo-600 border border-indigo-100">
                      <Users className="h-5 w-5" />
                    </div>
                    <div className="ml-4">
                      <div className="text-sm font-bold text-slate-900">{agent.name}</div>
                      <div className="text-[10px] font-mono text-slate-400 bg-slate-100 px-1.5 py-0.5 rounded mt-0.5 inline-block">{agent.id}</div>
                    </div>
                  </div>
                </td>
                <td className="px-6 py-5 whitespace-nowrap text-xs font-bold">
                  <span className={`px-2 py-1 rounded-md border ${
                    agent.role === 'manager' ? 'bg-purple-50 border-purple-100 text-purple-700' :
                    agent.role === 'worker' ? 'bg-blue-50 border-blue-100 text-blue-700' :
                    'bg-slate-50 border-slate-100 text-slate-600'
                  }`}>
                    {agent.role}
                  </span>
                </td>
                <td className="px-6 py-5 max-w-xs">
                  <p className="text-xs text-slate-600 truncate italic" title={agent.mission}>
                    {agent.mission || 'No mission assigned'}
                  </p>
                </td>
                <td className="px-6 py-5 whitespace-nowrap">
                  <span className={`inline-flex items-center px-2.5 py-1 rounded-full text-[10px] font-black uppercase tracking-wider border ${
                    agent.status === 'online' ? 'bg-green-50 text-green-700 border-green-200' :
                    agent.status === 'busy' ? 'bg-amber-50 text-amber-700 border-amber-200' :
                    'bg-slate-50 text-slate-600 border-slate-200'
                  }`}>
                    <Circle className={`mr-1.5 h-1.5 w-1.5 fill-current`} />
                    {agent.status}
                  </span>
                </td>
                <td className="px-6 py-5 whitespace-nowrap text-right text-sm font-medium">
                  <button 
                    onClick={() => handleOpenDetails(agent)}
                    className="text-indigo-600 hover:text-indigo-900 bg-indigo-50 hover:bg-indigo-100 px-3 py-1.5 rounded-lg transition-all font-bold text-xs"
                  >
                    Details
                  </button>
                </td>
              </tr>
            ))}
            {agents.length === 0 && (
              <tr>
                <td colSpan={5} className="px-6 py-12 text-center text-slate-500 italic">
                  No agents registered yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* Create Modal */}
      {isModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/50 backdrop-blur-sm">
          <div className="bg-white rounded-xl shadow-2xl w-full max-w-md overflow-hidden ring-1 ring-black/5">
            <div className="px-6 py-4 border-b border-slate-200 flex justify-between items-center bg-slate-50">
              <h3 className="text-lg font-bold text-slate-900">Add New Agent</h3>
              <button onClick={() => setIsModalOpen(false)} className="text-slate-500 hover:text-slate-700 p-1 rounded-full hover:bg-slate-200 transition-colors">
                <X className="h-5 w-5" />
              </button>
            </div>
            <form onSubmit={handleSubmit} className="p-6 space-y-4">
              {error && (
                <div className="p-3 bg-red-50 border border-red-200 text-red-700 text-sm rounded-md italic">
                  {error}
                </div>
              )}
              <div className="space-y-4">
                <div>
                  <label className="block text-xs font-bold text-slate-500 uppercase tracking-wider mb-1">Agent ID</label>
                  <input
                    type="text"
                    required
                    placeholder="e.g. worker-01"
                    className="w-full px-3 py-2 bg-slate-50 border border-slate-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 text-sm"
                    value={formData.id}
                    onChange={(e) => setFormData({ ...formData, id: e.target.value })}
                  />
                </div>
                <div>
                  <label className="block text-xs font-bold text-slate-500 uppercase tracking-wider mb-1">Name</label>
                  <input
                    type="text"
                    required
                    placeholder="e.g. Simulation Worker"
                    className="w-full px-3 py-2 bg-slate-50 border border-slate-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 text-sm"
                    value={formData.name}
                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  />
                </div>
                <div>
                  <label className="block text-xs font-bold text-slate-500 uppercase tracking-wider mb-1">Role</label>
                  <select
                    className="w-full px-3 py-2 bg-slate-50 border border-slate-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 text-sm"
                    value={formData.role}
                    onChange={(e) => setFormData({ ...formData, role: e.target.value })}
                  >
                    <option value="worker">Worker</option>
                    <option value="manager">Manager</option>
                    <option value="observer">Observer</option>
                  </select>
                </div>
                <div>
                  <label className="block text-xs font-bold text-slate-500 uppercase tracking-wider mb-1">Contribution Mission</label>
                  <textarea
                    placeholder="How does this agent contribute to the team's goals?"
                    className="w-full px-3 py-2 bg-slate-50 border border-slate-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 text-sm h-24 resize-none"
                    value={formData.mission}
                    onChange={(e) => setFormData({ ...formData, mission: e.target.value })}
                  />
                </div>
              </div>
              <div className="pt-4 flex justify-end space-x-3">
                <button
                  type="button"
                  onClick={() => setIsModalOpen(false)}
                  className="px-4 py-2 text-sm font-medium text-slate-700 bg-white border border-slate-300 rounded-md hover:bg-slate-50 transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={isSubmitting}
                  className="px-4 py-2 text-sm font-bold text-white bg-indigo-600 rounded-md hover:bg-indigo-700 disabled:opacity-50 shadow-sm transition-all active:transform active:scale-95"
                >
                  {isSubmitting ? 'Creating...' : 'Create Agent'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Team Edit Modal */}
      {isTeamModalOpen && selectedTeam && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/50 backdrop-blur-sm">
          <div className="bg-white rounded-xl shadow-2xl w-full max-w-lg overflow-hidden ring-1 ring-black/5">
            <div className="px-6 py-4 border-b border-slate-200 flex justify-between items-center bg-indigo-600 text-white">
              <div className="flex items-center space-x-3">
                <LayoutGrid className="h-5 w-5" />
                <h3 className="text-lg font-bold">Edit Team Mission</h3>
              </div>
              <button onClick={() => setIsTeamModalOpen(false)} className="text-white/70 hover:text-white p-1 rounded-full hover:bg-white/10 transition-colors">
                <X className="h-5 w-5" />
              </button>
            </div>
            
            <form onSubmit={handleUpdateTeam} className="p-6 space-y-6">
              <div className="space-y-4">
                <div className="flex items-center space-x-2 text-indigo-700 mb-2">
                  <Target className="h-4 w-4" />
                  <h4 className="text-sm font-bold uppercase tracking-tight">Overall Team Mission</h4>
                </div>
                <textarea
                  placeholder="Define the core purpose and goals of this team..."
                  className="w-full px-4 py-3 bg-slate-50 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-indigo-500 text-sm h-40 resize-none shadow-inner leading-relaxed"
                  value={teamFormData.mission}
                  onChange={(e) => setTeamFormData({ ...teamFormData, mission: e.target.value })}
                />
              </div>

              <div className="pt-4 flex justify-end space-x-3 border-t border-slate-100 mt-6">
                <button
                  type="button"
                  onClick={() => setIsTeamModalOpen(false)}
                  className="px-6 py-2 text-sm font-medium text-slate-600 hover:text-slate-800"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={isSubmitting}
                  className="px-6 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 font-bold flex items-center shadow-md active:transform active:scale-95 transition-all"
                >
                  <Save className="h-4 w-4 mr-2" />
                  Save Team Mission
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Detail/Edit Modal */}
      {isDetailModalOpen && selectedAgent && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/50 backdrop-blur-sm">
          <div className="bg-white rounded-xl shadow-2xl w-full max-w-lg overflow-hidden ring-1 ring-black/5">
            <div className="px-6 py-4 border-b border-slate-200 flex justify-between items-center bg-indigo-600 text-white">
              <div className="flex items-center space-x-3">
                <div className="p-2 bg-white/20 rounded-lg">
                  <Info className="h-5 w-5" />
                </div>
                <div>
                  <h3 className="text-lg font-bold leading-none">Agent Details</h3>
                  <p className="text-indigo-100 text-[10px] mt-1 uppercase tracking-widest font-bold">{selectedAgent.id}</p>
                </div>
              </div>
              <button onClick={() => setIsDetailModalOpen(false)} className="text-white/70 hover:text-white p-1 rounded-full hover:bg-white/10 transition-colors">
                <X className="h-5 w-5" />
              </button>
            </div>
            
            <form onSubmit={handleUpdateAgent} className="p-6 space-y-6">
              {error && (
                <div className="p-3 bg-red-50 border border-red-200 text-red-700 text-sm rounded-md italic">
                  {error}
                </div>
              )}

              <div className="grid grid-cols-1 gap-6">
                <div className="space-y-4">
                  <div className="flex items-center space-x-2 text-indigo-700 mb-2">
                    <Shield className="h-4 w-4" />
                    <h4 className="text-sm font-bold uppercase tracking-tight">System Role</h4>
                  </div>
                  <div className="grid grid-cols-3 gap-3">
                    {['manager', 'worker', 'observer'].map((r) => (
                      <button
                        key={r}
                        type="button"
                        onClick={() => setDetailFormData({ ...detailFormData, role: r })}
                        className={`py-2 px-3 rounded-lg border text-xs font-bold capitalize transition-all ${
                          detailFormData.role === r 
                            ? 'bg-indigo-600 border-indigo-600 text-white shadow-md' 
                            : 'bg-white border-slate-200 text-slate-600 hover:border-indigo-300 hover:bg-indigo-50'
                        }`}
                      >
                        {r}
                      </button>
                    ))}
                  </div>
                </div>

                <div className="space-y-4">
                  <div className="flex items-center space-x-2 text-indigo-700 mb-2">
                    <Target className="h-4 w-4" />
                    <h4 className="text-sm font-bold uppercase tracking-tight">Contribution Mission</h4>
                  </div>
                  <textarea
                    placeholder="Describe how this agent contributes to the team mission..."
                    className="w-full px-4 py-3 bg-slate-50 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-indigo-500 text-sm h-32 resize-none shadow-inner leading-relaxed"
                    value={detailFormData.mission}
                    onChange={(e) => setDetailFormData({ ...detailFormData, mission: e.target.value })}
                  />
                  <p className="text-[10px] text-slate-400 italic">
                    This mission will be visible to the agent via masatools SDK.
                  </p>
                </div>
              </div>

              <div className="pt-4 flex justify-end space-x-3 border-t border-slate-100 mt-6">
                <button
                  type="button"
                  onClick={() => setIsDetailModalOpen(false)}
                  className="px-6 py-2 text-sm font-medium text-slate-600 hover:text-slate-800 transition-colors"
                >
                  Close
                </button>
                <button
                  type="submit"
                  disabled={isSubmitting}
                  className="px-6 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 font-bold flex items-center shadow-md active:transform active:scale-95 transition-all disabled:opacity-50"
                >
                  <Save className="h-4 w-4 mr-2" />
                  {isSubmitting ? 'Saving...' : 'Save Changes'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
