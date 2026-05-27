'use client';

import { useStore } from '@/store/useStore';
import { 
  MessageSquare, 
  User, 
  Clock, 
  Hash,
  Send,
  Layers
} from 'lucide-react';
import { useState, useMemo } from 'react';

export default function BoardPage() {
  const messages = useStore((state) => state.messages);
  const agents = useStore((state) => state.agents);
  const threads = useStore((state) => state.threads);
  
  const [command, setCommand] = useState('');
  const [selectedThread, setSelectedThread] = useState<string>('new');
  const [selectedAgent, setSelectedAgent] = useState<string>('all');
  const [isSubmitting, setIsSubmitting] = useState(false);

  // Group messages by thread_id
  const groupedMessages = useMemo(() => {
    const groups: Record<string, typeof messages> = {};
    const noThread: typeof messages = [];

    messages.forEach(msg => {
      if (msg.thread_id) {
        if (!groups[msg.thread_id]) groups[msg.thread_id] = [];
        groups[msg.thread_id].push(msg);
      } else {
        noThread.push(msg);
      }
    });

    // Sort messages within each group by timestamp (ascending)
    Object.keys(groups).forEach(tid => {
      groups[tid].sort((a, b) => a.timestamp - b.timestamp);
    });

    return { groups, noThread };
  }, [messages]);

  const handlePostTask = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!command.trim()) return;

    setIsSubmitting(true);
    const apiUrl = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
    try {
      const payload: any = {
        command,
        created_by_agent: 'admin-ui',
        deadline: new Date(Date.now() + 3600000).toISOString(),
      };

      if (selectedThread !== 'new') {
        payload.parent_thread_id = selectedThread;
      }

      if (selectedAgent !== 'all') {
        payload.to = [selectedAgent];
      }

      const response = await fetch(`${apiUrl}/api/v1/threads`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (response.ok) {
        setCommand('');
      }
    } catch (err) {
      console.error('Failed to post task:', err);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="flex flex-col h-[calc(100vh-4rem)]">
      <header className="mb-6">
        <h1 className="text-3xl font-extrabold text-slate-900 tracking-tight">Message Board</h1>
        <p className="mt-2 text-lg text-slate-700 font-medium">Grouped by Thread ID</p>
      </header>

      {/* Main Board Area */}
      <div className="flex-1 overflow-y-auto space-y-8 pb-12 pr-2">
        {Object.entries(groupedMessages.groups).map(([threadId, threadMessages]) => (
          <section key={threadId} className="bg-white shadow-sm ring-1 ring-slate-200 rounded-lg overflow-hidden border-t-4 border-indigo-500">
            <div className="bg-slate-50 px-4 py-2 border-b border-slate-200 flex justify-between items-center">
              <div className="flex items-center text-indigo-700 font-bold">
                <Layers className="h-4 w-4 mr-2" />
                <span className="text-sm font-mono">Thread: {threadId}</span>
              </div>
              <span className="text-xs text-slate-500">{threadMessages.length} messages</span>
            </div>
            <div className="p-4 space-y-4">
              {threadMessages.map((message, idx) => (
                <MessageItem key={idx} message={message} />
              ))}
            </div>
          </section>
        ))}

        {groupedMessages.noThread.length > 0 && (
          <section className="bg-white shadow-sm ring-1 ring-slate-200 rounded-lg overflow-hidden border-t-4 border-slate-400">
            <div className="bg-slate-50 px-4 py-2 border-b border-slate-200">
              <span className="text-sm font-bold text-slate-600 uppercase tracking-tight">General Updates</span>
            </div>
            <div className="p-4 space-y-4">
              {groupedMessages.noThread.map((message, idx) => (
                <MessageItem key={idx} message={message} />
              ))}
            </div>
          </section>
        )}

        {messages.length === 0 && (
          <div className="flex flex-col items-center justify-center py-24 text-slate-400 bg-white rounded-lg shadow-sm border border-slate-200 border-dashed">
            <MessageSquare className="h-12 w-12 mb-4 opacity-20" />
            <p className="italic text-lg font-light">No messages on the board yet.</p>
          </div>
        )}
      </div>

      {/* Quick Input Bar at the Bottom */}
      <div className="mt-4 bg-white p-4 shadow-lg ring-1 ring-slate-200 rounded-t-xl border-t-2 border-indigo-500 sticky bottom-0">
        <form onSubmit={handlePostTask} className="space-y-3">
          <div className="flex gap-4 items-center">
            <div className="flex-1 flex gap-2">
              <select 
                value={selectedThread}
                onChange={(e) => setSelectedThread(e.target.value)}
                className="text-xs border border-slate-300 rounded px-2 py-1 bg-slate-50 focus:ring-1 focus:ring-indigo-500 outline-none text-slate-700"
              >
                <option value="new">New Thread</option>
                {threads.map(t => (
                  <option key={t.id} value={t.id}>Sub-task of {t.id.substring(0,8)}...</option>
                ))}
              </select>

              <select 
                value={selectedAgent}
                onChange={(e) => setSelectedAgent(e.target.value)}
                className="text-xs border border-slate-300 rounded px-2 py-1 bg-slate-50 focus:ring-1 focus:ring-indigo-500 outline-none text-slate-700"
              >
                <option value="all">To: All Agents</option>
                {agents.map(a => (
                  <option key={a.id} value={a.id}>To: {a.name}</option>
                ))}
              </select>
            </div>
          </div>

          <div className="flex gap-4">
            <div className="flex-1">
              <input
                type="text"
                value={command}
                onChange={(e) => setCommand(e.target.value)}
                placeholder="Post a new task to all agents..."
                className="w-full px-4 py-3 bg-slate-50 border border-slate-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-indigo-500 text-slate-900 shadow-inner"
                disabled={isSubmitting}
              />
            </div>
            <button
              type="submit"
              disabled={isSubmitting || !command.trim()}
              className="bg-indigo-600 text-white px-6 py-2 rounded-lg hover:bg-indigo-700 transition-all flex items-center font-bold shadow-md active:transform active:scale-95 disabled:opacity-50"
            >
              {isSubmitting ? '...' : <Send className="h-5 w-5" />}
              <span className="ml-2 hidden sm:inline">Post Task</span>
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function MessageItem({ message }: { message: any }) {
  return (
    <div className="border-b border-slate-100 last:border-0 pb-4 last:pb-0">
      <div className="flex justify-between items-start mb-2">
        <div className="flex items-center space-x-3">
          <span className={`px-2 py-0.5 rounded text-[10px] font-black uppercase ${
            message.type === 'task' ? 'bg-blue-600 text-white' :
            message.type === 'result' ? 'bg-green-600 text-white' :
            message.type === 'error' ? 'bg-red-600 text-white' :
            message.type === 'status' ? 'bg-amber-500 text-white' :
            'bg-slate-500 text-white'
          }`}>
            {message.type}
          </span>
          <div className="flex items-center text-sm text-slate-900 font-bold">
            <User className="h-3 w-3 mr-1 text-slate-400" />
            {message.from}
          </div>
        </div>
        <div className="flex items-center text-[10px] text-slate-400 font-medium">
          <Clock className="h-3 w-3 mr-1" />
          {new Date(message.timestamp * 1000).toLocaleTimeString()}
        </div>
      </div>
      <div className="text-sm text-slate-800 ml-4 border-l-2 border-slate-100 pl-4">
        <pre className="bg-slate-50 text-slate-700 p-3 rounded-md overflow-x-auto font-mono text-xs border border-slate-200">
          {JSON.stringify(message.payload, null, 2)}
        </pre>
      </div>
    </div>
  );
}
