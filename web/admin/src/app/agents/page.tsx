'use client';

import { useStore } from '@/store/useStore';
import { 
  Users, 
  Circle,
  Plus,
  X
} from 'lucide-react';
import { useState } from 'react';

export default function AgentsPage() {
  const agents = useStore((state) => state.agents);
  const setAgents = useStore((state) => state.setAgents);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [formData, setFormData] = useState({
    id: '',
    name: '',
    role: 'worker',
    team_id: '01H0V6P6V6P6V6P6V6P6V6P6V6' // Default Core Team
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
      setFormData({ id: '', name: '', role: 'worker', team_id: '01H0V6P6V6P6V6P6V6P6V6P6V6' });
      refreshAgents();
    } catch (err: any) {
      setError(err.message);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div>
      <header className="mb-8 flex justify-between items-center">
        <div>
          <h1 className="text-3xl font-extrabold text-slate-900 tracking-tight">Agents</h1>
          <p className="mt-2 text-lg text-slate-700">Monitor and manage all autonomous agents in the system.</p>
        </div>
        <button 
          onClick={() => setIsModalOpen(true)}
          className="bg-indigo-600 text-white px-4 py-2 rounded-md hover:bg-indigo-700 transition-colors flex items-center"
        >
          <Plus className="mr-2 h-4 w-4" />
          Add Agent
        </button>
      </header>

      {/* Modal */}
      {isModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/50 backdrop-blur-sm">
          <div className="bg-white rounded-lg shadow-xl w-full max-w-md overflow-hidden">
            <div className="px-6 py-4 border-b border-slate-200 flex justify-between items-center bg-slate-50">
              <h3 className="text-lg font-bold text-slate-900">Add New Agent</h3>
              <button onClick={() => setIsModalOpen(false)} className="text-slate-500 hover:text-slate-700">
                <X className="h-5 w-5" />
              </button>
            </div>
            <form onSubmit={handleSubmit} className="p-6 space-y-4">
              {error && (
                <div className="p-3 bg-red-50 border border-red-200 text-red-700 text-sm rounded-md">
                  {error}
                </div>
              )}
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1">Agent ID</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. worker-01"
                  className="w-full px-3 py-2 border border-slate-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  value={formData.id}
                  onChange={(e) => setFormData({ ...formData, id: e.target.value })}
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1">Name</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. Simulation Worker"
                  className="w-full px-3 py-2 border border-slate-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  value={formData.name}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1">Role</label>
                <select
                  className="w-full px-3 py-2 border border-slate-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  value={formData.role}
                  onChange={(e) => setFormData({ ...formData, role: e.target.value })}
                >
                  <option value="worker">Worker</option>
                  <option value="manager">Manager</option>
                  <option value="observer">Observer</option>
                </select>
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-700 mb-1">Team ID</label>
                <input
                  type="text"
                  placeholder="ULID (Optional)"
                  className="w-full px-3 py-2 border border-slate-300 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  value={formData.team_id}
                  onChange={(e) => setFormData({ ...formData, team_id: e.target.value })}
                />
              </div>
              <div className="pt-4 flex justify-end space-x-3">
                <button
                  type="button"
                  onClick={() => setIsModalOpen(false)}
                  className="px-4 py-2 text-sm font-medium text-slate-700 bg-white border border-slate-300 rounded-md hover:bg-slate-50"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={isSubmitting}
                  className="px-4 py-2 text-sm font-medium text-white bg-indigo-600 rounded-md hover:bg-indigo-700 disabled:opacity-50"
                >
                  {isSubmitting ? 'Creating...' : 'Create Agent'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      <div className="bg-white shadow-sm ring-1 ring-slate-200 rounded-lg overflow-hidden">
        <table className="min-w-full divide-y divide-slate-200">
          <thead className="bg-slate-50">
            <tr>
              <th scope="col" className="px-6 py-3 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">Name</th>
              <th scope="col" className="px-6 py-3 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">Role</th>
              <th scope="col" className="px-6 py-3 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">Status</th>
              <th scope="col" className="px-6 py-3 text-left text-xs font-medium text-slate-500 uppercase tracking-wider">Last Updated</th>
              <th scope="col" className="relative px-6 py-3">
                <span className="sr-only">Edit</span>
              </th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-slate-200">
            {agents.map((agent) => (
              <tr key={agent.id}>
                <td className="px-6 py-4 whitespace-nowrap">
                  <div className="flex items-center">
                    <div className="h-10 w-10 flex-shrink-0 flex items-center justify-center bg-indigo-100 rounded-full text-indigo-600">
                      <Users className="h-6 w-6" />
                    </div>
                    <div className="ml-4">
                      <div className="text-sm font-medium text-slate-900">{agent.name}</div>
                      <div className="text-sm text-slate-500">{agent.id}</div>
                    </div>
                  </div>
                </td>
                <td className="px-6 py-4 whitespace-nowrap text-sm text-slate-500">
                  <span className="capitalize">{agent.role}</span>
                </td>
                <td className="px-6 py-4 whitespace-nowrap">
                  <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
                    agent.status === 'online' ? 'bg-green-100 text-green-800' :
                    agent.status === 'busy' ? 'bg-amber-100 text-amber-800' :
                    'bg-slate-100 text-slate-800'
                  }`}>
                    <Circle className={`mr-1.5 h-2 w-2 fill-current`} />
                    {agent.status}
                  </span>
                </td>
                <td className="px-6 py-4 whitespace-nowrap text-sm text-slate-500">
                  {new Date(agent.updated_at).toLocaleString()}
                </td>
                <td className="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                  <button className="text-indigo-600 hover:text-indigo-900">Details</button>
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
    </div>
  );
}
