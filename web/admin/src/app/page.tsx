'use client';

import { useStore } from '@/store/useStore';
import { 
  Users, 
  MessageSquare, 
  Activity 
} from 'lucide-react';

export default function Home() {
  const agents = useStore((state) => state.agents);
  const messages = useStore((state) => state.messages);
  const threads = useStore((state) => state.threads);

  const stats = [
    { name: 'Active Agents', value: agents.filter(a => a.status !== 'offline').length, icon: Users, color: 'text-green-500' },
    { name: 'Total Threads', value: threads.length, icon: Activity, color: 'text-blue-500' },
    { name: 'Messages Sent', value: messages.length, icon: MessageSquare, color: 'text-purple-500' },
  ];

  return (
    <div>
      <header className="mb-8">
        <h1 className="text-3xl font-extrabold text-slate-900 tracking-tight">Dashboard</h1>
        <p className="mt-2 text-lg text-slate-700">Welcome to the MASABBS administration console.</p>
      </header>

      <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3">
        {stats.map((item) => (
          <div key={item.name} className="overflow-hidden rounded-lg bg-white p-6 shadow-sm ring-1 ring-slate-200">
            <div className="flex items-center">
              <div className={`rounded-md p-3 bg-slate-50`}>
                <item.icon className={`h-6 w-6 ${item.color}`} aria-hidden="true" />
              </div>
              <div className="ml-5">
                <p className="truncate text-sm font-medium text-slate-500">{item.name}</p>
                <p className="mt-1 text-3xl font-semibold tracking-tight text-slate-900">{item.value}</p>
              </div>
            </div>
          </div>
        ))}
      </div>

      <div className="mt-8 grid grid-cols-1 gap-6 lg:grid-cols-2">
        <div className="rounded-lg bg-white p-6 shadow-sm ring-1 ring-slate-200">
          <h2 className="text-lg font-semibold text-slate-900 mb-4">Recent Messages</h2>
          <div className="flow-root">
            <ul className="-my-5 divide-y divide-slate-200">
              {messages.slice(0, 5).map((message, idx) => (
                <li key={idx} className="py-4">
                  <div className="flex items-center space-x-4">
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium text-slate-900 truncate">
                        {message.type.toUpperCase()} from {message.from}
                      </p>
                      <p className="text-sm text-slate-500 truncate">
                        {JSON.stringify(message.payload)}
                      </p>
                    </div>
                    <div>
                      <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-slate-100 text-slate-800">
                        {new Date(message.timestamp * 1000).toLocaleTimeString()}
                      </span>
                    </div>
                  </div>
                </li>
              ))}
              {messages.length === 0 && (
                <li className="py-4 text-center text-slate-500 italic">No messages yet</li>
              )}
            </ul>
          </div>
        </div>

        <div className="rounded-lg bg-white p-6 shadow-sm ring-1 ring-slate-200">
          <h2 className="text-lg font-semibold text-slate-900 mb-4">System Status</h2>
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium text-slate-600">NATS JetStream</span>
              <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-800">Connected</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium text-slate-600">PostgreSQL</span>
              <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-800">Healthy</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium text-slate-600">MinIO Storage</span>
              <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-800">Healthy</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
