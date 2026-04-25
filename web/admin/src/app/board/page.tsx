'use client';

import { useStore } from '@/store/useStore';
import { 
  MessageSquare, 
  User, 
  Clock, 
  Hash 
} from 'lucide-react';

export default function BoardPage() {
  const messages = useStore((state) => state.messages);

  return (
    <div className="flex flex-col h-[calc(100vh-8rem)]">
      <header className="mb-8">
        <h1 className="text-3xl font-extrabold text-slate-900 tracking-tight">Message Board</h1>
        <p className="mt-2 text-lg text-slate-700">Real-time system communication and task updates.</p>
      </header>

      <div className="flex-1 overflow-y-auto bg-white shadow-sm ring-1 ring-slate-200 rounded-lg p-4 space-y-4">
        {messages.map((message, idx) => (
          <div key={idx} className="border-l-4 border-indigo-500 bg-slate-50 p-4 rounded-r-md">
            <div className="flex justify-between items-start mb-2">
              <div className="flex items-center space-x-4">
                <span className={`px-2 py-1 rounded text-xs font-bold uppercase ${
                  message.type === 'task' ? 'bg-blue-100 text-blue-700' :
                  message.type === 'result' ? 'bg-green-100 text-green-700' :
                  message.type === 'error' ? 'bg-red-100 text-red-700' :
                  'bg-slate-200 text-slate-700'
                }`}>
                  {message.type}
                </span>
                <div className="flex items-center text-sm text-slate-600">
                  <User className="h-4 w-4 mr-1" />
                  <span className="font-medium">{message.from}</span>
                </div>
                {message.thread_id && (
                  <div className="flex items-center text-sm text-slate-600">
                    <Hash className="h-4 w-4 mr-1" />
                    <span className="font-mono text-xs">{message.thread_id}</span>
                  </div>
                )}
              </div>
              <div className="flex items-center text-xs text-slate-400">
                <Clock className="h-3 w-3 mr-1" />
                {new Date(message.timestamp * 1000).toLocaleTimeString()}
              </div>
            </div>
            <div className="text-sm text-slate-800">
              <pre className="bg-slate-900 text-slate-100 p-3 rounded mt-2 overflow-x-auto font-mono text-xs">
                {JSON.stringify(message.payload, null, 2)}
              </pre>
            </div>
          </div>
        ))}
        {messages.length === 0 && (
          <div className="flex flex-col items-center justify-center h-full text-slate-400">
            <MessageSquare className="h-12 w-12 mb-4 opacity-20" />
            <p className="italic text-lg font-light">Listening for messages...</p>
          </div>
        )}
      </div>
    </div>
  );
}
