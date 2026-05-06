'use client';

import { useState } from 'react';
import { PlayCircle, AlertCircle } from 'lucide-react';

export default function OperationsPage() {
  const [command, setCommand] = useState('');
  const [agentId, setAgentId] = useState('admin-ui');
  const [deadline, setDeadline] = useState('');
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState<{ type: 'success' | 'error', text: string } | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setMessage(null);

    try {
      const response = await fetch(`${process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080'}/api/v1/threads`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          command,
          created_by_agent: agentId,
          deadline: deadline || new Date(Date.now() + 3600000).toISOString(), // Default 1h
        }),
      });

      if (!response.ok) {
        throw new Error('Failed to create thread');
      }

      const data = await response.json();
      setMessage({ type: 'success', text: `Thread created successfully! ID: ${data.thread_id}` });
      setCommand('');
    } catch (err) {
      setMessage({ type: 'error', text: err instanceof Error ? err.message : 'An error occurred' });
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="max-w-2xl mx-auto">
      <header className="mb-8 text-center">
        <h1 className="text-3xl font-extrabold text-slate-900 tracking-tight">System Operations</h1>
        <p className="mt-2 text-lg text-slate-700">Initiate new tasks and system-wide commands.</p>
      </header>

      <div className="bg-white shadow-sm ring-1 ring-slate-200 rounded-lg p-8">
        <h2 className="text-lg font-semibold text-slate-900 mb-6 flex items-center">
          <PlayCircle className="h-5 w-5 mr-2 text-indigo-600" />
          Create New Thread / Task
        </h2>

        {message && (
          <div className={`mb-6 p-4 rounded-md flex items-center ${
            message.type === 'success' ? 'bg-green-50 text-green-700 border border-green-200' : 'bg-red-50 text-red-700 border border-red-200'
          }`}>
            {message.type === 'error' && <AlertCircle className="h-5 w-5 mr-2" />}
            {message.text}
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-6">
          <div>
            <label htmlFor="command" className="block text-sm font-medium text-slate-800">
              Command / Task Instruction
            </label>
            <textarea
              id="command"
              rows={4}
              required
              className="mt-1 block w-full rounded-md border-slate-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm p-3 border bg-white text-slate-900 placeholder-slate-400"
              placeholder="Describe the task for agents..."
              value={command}
              onChange={(e) => setCommand(e.target.value)}
            />
          </div>

          <div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
            <div>
              <label htmlFor="agent-id" className="block text-sm font-medium text-slate-800">
                Created By (Agent ID)
              </label>
              <input
                type="text"
                id="agent-id"
                required
                className="mt-1 block w-full rounded-md border-slate-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm p-2 border bg-white text-slate-900"
                value={agentId}
                onChange={(e) => setAgentId(e.target.value)}
              />
            </div>
            <div>
              <label htmlFor="deadline" className="block text-sm font-medium text-slate-800">
                Deadline (ISO8601)
              </label>
              <input
                type="datetime-local"
                id="deadline"
                className="mt-1 block w-full rounded-md border-slate-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm p-2 border bg-white text-slate-900"
                onChange={(e) => setDeadline(new Date(e.target.value).toISOString())}
              />
            </div>
          </div>

          <div className="pt-4">
            <button
              type="submit"
              disabled={loading}
              className={`w-full flex justify-center py-3 px-4 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-indigo-600 hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500 ${
                loading ? 'opacity-50 cursor-not-allowed' : ''
              }`}
            >
              {loading ? 'Processing...' : 'Publish Task'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
