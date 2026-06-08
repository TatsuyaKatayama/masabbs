'use client';

import { useStore } from '@/store/useStore';
import { 
  MessageSquare, 
  User, 
  Clock, 
  Send,
  Layers,
  ChevronDown,
  ChevronRight,
  Trash2,
  Eye,
  X,
  FileText,
  Download,
  Image as ImageIcon,
  Loader2
} from 'lucide-react';
import { useState, useMemo, useEffect, useCallback } from 'react';
import { MessageEnvelope } from '@/types';
import Image from 'next/image';
import TeamSwitcher from '@/components/TeamSwitcher';

export default function BoardPage() {
  const messages = useStore((state) => state.messages);
  const setMessages = useStore((state) => state.setMessages);
  const threads = useStore((state) => state.threads);
  const setThreads = useStore((state) => state.setThreads);
  const selectedTeamId = useStore((state) => state.selectedTeamId);
  
  const [command, setCommand] = useState('');
  const [threadIdInput, setThreadIdInput] = useState('');
  const [toAgentsInput, setToAgentsInput] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [collapsedThreads, setCollapsedThreads] = useState<Record<string, boolean>>({});
  const [previewThread, setPreviewThread] = useState<string | null>(null);

  const toggleCollapse = (threadId: string) => {
    setCollapsedThreads(prev => ({
      ...prev,
      [threadId]: !prev[threadId]
    }));
  };

  const handleDeleteThread = async (threadId: string) => {
    if (!confirm('Are you sure you want to delete this thread and all its messages?')) return;

    const apiUrl = process.env.NEXT_PUBLIC_API_URL || '';
    try {
      const response = await fetch(`${apiUrl}/api/v1/threads/${threadId}`, {
        method: 'DELETE',
      });

      if (response.ok) {
        // Refresh local state
        setMessages(messages.filter(m => m.thread_id !== threadId));
        setThreads(threads.filter(t => t.id !== threadId));
      } else {
        alert('Failed to delete thread');
      }
    } catch (err) {
      console.error('Delete error:', err);
    }
  };

  const getThreadText = (threadId: string) => {
    const threadMsgs = messages.filter(m => m.thread_id === threadId).sort((a,b) => a.timestamp - b.timestamp);
    return threadMsgs.map(m => {
      const time = new Date(m.timestamp * 1000).toLocaleTimeString();
      let content = '';
      if (m.type === 'task') content = m.payload.command as string;
      else content = JSON.stringify(m.payload);
      
      return `[${time}] ${m.from}: ${content}`;
    }).join('\n');
  };

  const visibleThreadIds = useMemo(() => {
    return new Set(
      threads
        .filter((thread) => !selectedTeamId || thread.team_id === selectedTeamId)
        .map((thread) => thread.id)
    );
  }, [threads, selectedTeamId]);

  const visibleMessages = useMemo(() => {
    if (!selectedTeamId) return messages;
    return messages.filter((message) => message.thread_id && visibleThreadIds.has(message.thread_id));
  }, [messages, selectedTeamId, visibleThreadIds]);

  // Group messages by thread_id
  const groupedMessages = useMemo(() => {
    const groups: Record<string, typeof visibleMessages> = {};
    const noThread: typeof visibleMessages = [];

    visibleMessages.forEach(msg => {
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
  }, [visibleMessages]);

  const handlePostTask = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!command.trim()) return;

    setIsSubmitting(true);
    const apiUrl = process.env.NEXT_PUBLIC_API_URL || '';
    try {
      const payload: Record<string, unknown> = {
        command,
        created_by_agent: 'admin-ui',
        deadline: new Date(Date.now() + 3600000).toISOString(),
      };

      if (threadIdInput.trim()) {
        payload.thread_id = threadIdInput.trim();
      }

      if (toAgentsInput.trim()) {
        payload.to = toAgentsInput.split(',').map(s => s.trim()).filter(s => s !== '');
      }

      if (selectedTeamId) {
        payload.team_id = selectedTeamId;
      }

      const response = await fetch(`${apiUrl}/api/v1/threads`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (response.ok) {
        setCommand('');
      } else {
        const errData = await response.json();
        alert(`Error: ${errData.error}`);
      }
    } catch (err) {
      console.error('Failed to post task:', err);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="flex flex-col h-[calc(100vh-4rem)]">
      <header className="mb-6 flex items-start justify-between">
        <div>
          <h1 className="text-3xl font-extrabold text-slate-900 tracking-tight">Message Board</h1>
          <p className="mt-2 text-lg text-slate-700 font-medium">Grouped by Thread ID</p>
        </div>
        <TeamSwitcher />
      </header>

      {/* Main Board Area */}
      <div className="flex-1 overflow-y-auto space-y-8 pb-12 pr-2">
        {Object.entries(groupedMessages.groups).map(([threadId, threadMessages]) => (
          <section key={threadId} className="bg-white shadow-sm ring-1 ring-slate-200 rounded-lg overflow-hidden border-t-4 border-indigo-500 transition-all">
            <div 
              className="bg-slate-50 px-4 py-2 border-b border-slate-200 flex justify-between items-center cursor-pointer hover:bg-slate-100"
              onClick={() => toggleCollapse(threadId)}
            >
              <div className="flex items-center text-indigo-700 font-bold">
                {collapsedThreads[threadId] ? <ChevronRight className="h-4 w-4 mr-2" /> : <ChevronDown className="h-4 w-4 mr-2" />}
                <Layers className="h-4 w-4 mr-2" />
                <span className="text-sm font-mono">Thread: {threadId}</span>
              </div>
              <div className="flex items-center space-x-4">
                <span className="text-xs text-slate-500">{threadMessages.length} messages</span>
                <div className="flex items-center bg-white rounded-md border border-slate-200 overflow-hidden">
                  <button 
                    onClick={(e) => {
                      e.stopPropagation();
                      setPreviewThread(threadId);
                    }}
                    className="p-1.5 text-slate-500 hover:bg-slate-50 transition-colors border-r border-slate-200"
                    title="Preview History"
                  >
                    <Eye className="h-4 w-4" />
                  </button>
                  <button 
                    onClick={(e) => {
                      e.stopPropagation();
                      handleDeleteThread(threadId);
                    }}
                    className="p-1.5 text-slate-500 hover:text-red-500 hover:bg-slate-50 transition-colors"
                    title="Delete Thread"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </div>
            </div>
            {!collapsedThreads[threadId] && (
              <div className="p-4 space-y-4">
                {threadMessages.map((message, idx) => (
                  <MessageItem key={idx} message={message} />
                ))}
              </div>
            )}
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

        {visibleMessages.length === 0 && (
          <div className="flex flex-col items-center justify-center py-24 text-slate-400 bg-white rounded-lg shadow-sm border border-slate-200 border-dashed">
            <MessageSquare className="h-12 w-12 mb-4 opacity-20" />
            <p className="italic text-lg font-light">No messages on the board yet.</p>
          </div>
        )}
      </div>

      {/* Preview Modal */}
      {previewThread && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-sm">
          <div className="bg-white rounded-xl shadow-2xl w-full max-w-3xl max-h-[80vh] flex flex-col overflow-hidden ring-1 ring-black/5">
            <div className="px-6 py-4 border-b border-slate-200 flex justify-between items-center bg-slate-50">
              <div className="flex items-center space-x-2">
                <Eye className="h-5 w-5 text-indigo-600" />
                <h3 className="text-lg font-bold text-slate-900">Thread History Preview</h3>
                <span className="text-xs font-mono bg-slate-200 px-2 py-0.5 rounded text-slate-600">{previewThread}</span>
              </div>
              <button onClick={() => setPreviewThread(null)} className="p-1 rounded-full hover:bg-slate-200 text-slate-500 transition-colors">
                <X className="h-5 w-5" />
              </button>
            </div>
            <div className="flex-1 overflow-y-auto p-6 bg-slate-900">
              <pre className="text-emerald-400 font-mono text-sm leading-relaxed whitespace-pre-wrap">
                {getThreadText(previewThread)}
              </pre>
            </div>
            <div className="px-6 py-3 border-t border-slate-200 bg-slate-50 flex justify-end">
              <button
                onClick={() => setPreviewThread(null)}
                className="px-4 py-2 bg-white border border-slate-300 rounded-md text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm"
              >
                Close
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Quick Input Bar at the Bottom */}
      <div className="mt-4 bg-white p-4 shadow-lg ring-1 ring-slate-200 rounded-t-xl border-t-2 border-indigo-500 sticky bottom-0">
        <form onSubmit={handlePostTask} className="space-y-3">
          <div className="flex gap-4 items-center">
            <div className="flex-1 flex gap-2">
              <div className="flex items-center bg-slate-50 border border-slate-300 rounded px-2 py-1">
                <span className="text-[10px] font-bold text-slate-500 mr-2 uppercase">Thread ID:</span>
                <input 
                  type="text"
                  value={threadIdInput}
                  onChange={(e) => setThreadIdInput(e.target.value)}
                  placeholder="New Thread"
                  className="text-xs bg-transparent focus:outline-none text-slate-700 w-32 font-mono"
                />
              </div>

              <div className="flex items-center bg-slate-50 border border-slate-300 rounded px-2 py-1">
                <span className="text-[10px] font-bold text-slate-500 mr-2 uppercase">To:</span>
                <input 
                  type="text"
                  value={toAgentsInput}
                  onChange={(e) => setToAgentsInput(e.target.value)}
                  placeholder="agent1, agent2 (All if empty)"
                  className="text-xs bg-transparent focus:outline-none text-slate-700 w-48"
                />
              </div>
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

function MessageItem({ message }: { message: MessageEnvelope }) {
  return (
    <div className="border-b border-slate-100 last:border-0 pb-4 last:pb-0">
      <div className="flex justify-between items-start mb-2">
        <div className="flex items-center space-x-3">
          <span className={`px-2 py-0.5 rounded text-[10px] font-black uppercase ${
            message.type === 'task' ? 'bg-blue-600 text-white' :
            message.type === 'result' ? 'bg-green-600 text-white' :
            'bg-slate-500 text-white'
          }`}>
            {message.type}
          </span>
          <div className="flex items-center text-sm text-slate-900 font-bold">
            <User className="h-3 w-3 mr-1 text-slate-400" />
            {message.from}
          </div>
          {message.to && message.to.length > 0 && (
            <div className="flex items-center text-xs font-medium text-slate-500">
              <span className="mx-1 text-slate-300">→</span>
              <span className="bg-slate-100 px-1.5 py-0.5 rounded border border-slate-200">
                To: {message.to.join(', ')}
              </span>
            </div>
          )}
        </div>
        <div className="flex items-center text-[10px] text-slate-400 font-medium">
          <Clock className="h-3 w-3 mr-1" />
          {new Date(message.timestamp * 1000).toLocaleTimeString()}
        </div>
      </div>
      <div className="text-sm text-slate-800 ml-4 border-l-2 border-slate-100 pl-4 space-y-3">
        {message.type === 'result' && typeof message.payload.message === 'string' && (
          <div className="p-3 bg-green-50 text-green-800 rounded-md border border-green-100 font-medium italic">
            {message.payload.message as string}
          </div>
        )}

        <pre className="bg-slate-50 text-slate-700 p-3 rounded-md overflow-x-auto font-mono text-xs border border-slate-200">
          {JSON.stringify(message.payload, null, 2)}
        </pre>

        {message.type === 'result' && typeof message.payload.output_dir === 'string' && (
          <ResultArtifacts outputDir={message.payload.output_dir as string} />
        )}
      </div>
    </div>
  );
}

function ResultArtifacts({ outputDir }: { outputDir: string }) {
  const [files, setFiles] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    async function fetchFiles() {
      setIsLoading(true);
      setError(null);
      const apiUrl = process.env.NEXT_PUBLIC_API_URL || '';
      try {
        const response = await fetch(`${apiUrl}/api/v1/storage/files?prefix=${encodeURIComponent(outputDir)}`);
        if (!response.ok) throw new Error('Failed to fetch files');
        const data = await response.json();
        setFiles(data.files || []);
      } catch (err) {
        if (err instanceof Error) setError(err.message);
      } finally {
        setIsLoading(false);
      }
    }

    if (outputDir) {
      fetchFiles();
    }
  }, [outputDir]);

  if (isLoading) return (
    <div className="flex items-center space-x-2 text-xs text-slate-500 animate-pulse">
      <Loader2 className="h-3 w-3 animate-spin" />
      <span>Fetching artifacts from S3...</span>
    </div>
  );
  
  if (error) return <div className="text-xs text-red-500 italic">Error loading artifacts: {error}</div>;
  if (files.length === 0) return null;

  return (
    <div className="bg-white border border-slate-200 rounded-lg overflow-hidden shadow-sm max-w-2xl">
      <div className="bg-slate-50 px-3 py-1.5 border-b border-slate-200 flex items-center justify-between">
        <span className="text-[10px] font-bold text-slate-500 uppercase tracking-wider flex items-center">
          <Layers className="h-3 w-3 mr-1.5" />
          Artifacts in {outputDir}
        </span>
      </div>
      <div className="p-2 grid grid-cols-1 sm:grid-cols-2 gap-2">
        {files.map((file) => (
          <ArtifactItem key={file} fileKey={file} />
        ))}
      </div>
    </div>
  );
}

function ArtifactItem({ fileKey }: { fileKey: string }) {
  const [presignedUrl, setPresignedUrl] = useState<string | null>(null);
  const [isGettingUrl, setIsGettingUrl] = useState(false);
  
  const fileName = fileKey.split('/').pop() || fileKey;
  const isImage = /\.(png|jpe?g|gif|svg|webp)$/i.test(fileName);

  const getPresignedUrl = useCallback(async () => {
    if (presignedUrl) return presignedUrl;
    
    setIsGettingUrl(true);
    const apiUrl = process.env.NEXT_PUBLIC_API_URL || '';
    try {
      const response = await fetch(`${apiUrl}/api/v1/storage/presign?key=${encodeURIComponent(fileKey)}`);
      if (!response.ok) throw new Error('Failed to get URL');
      const data = await response.json();
      setPresignedUrl(data.url);
      return data.url;
    } catch (err) {
      console.error(err);
      return null;
    } finally {
      setIsGettingUrl(false);
    }
  }, [fileKey, presignedUrl]);

  useEffect(() => {
    let ignore = false;
    if (isImage && !presignedUrl && !isGettingUrl) {
      // Defer to avoid synchronous setState in effect
      Promise.resolve().then(() => {
        if (!ignore) getPresignedUrl();
      });
    }
    return () => { ignore = true; };
  }, [isImage, presignedUrl, isGettingUrl, getPresignedUrl]);

  return (
    <div className="flex flex-col border border-slate-100 rounded-md bg-slate-50/50 hover:bg-slate-50 transition-colors">
      <div className="flex items-center justify-between p-2">
        <div className="flex items-center min-w-0 flex-1">
          {isImage ? <ImageIcon className="h-4 w-4 mr-2 text-indigo-500 shrink-0" /> : <FileText className="h-4 w-4 mr-2 text-slate-400 shrink-0" />}
          <span className="text-xs font-medium text-slate-700 truncate" title={fileName}>{fileName}</span>
        </div>
        <div className="flex items-center space-x-1 ml-2">
          {presignedUrl ? (
            <a 
              href={presignedUrl} 
              target="_blank" 
              rel="noopener noreferrer"
              className="p-1 text-slate-400 hover:text-indigo-600 hover:bg-white rounded transition-all"
              title="Download/Open"
              download={fileName}
            >
              <Download className="h-3.5 w-3.5" />
            </a>
          ) : (
            <button 
              onClick={() => getPresignedUrl()}
              disabled={isGettingUrl}
              className="p-1 text-slate-400 hover:text-indigo-600 hover:bg-white rounded transition-all"
              title="Get Download Link"
            >
              {isGettingUrl ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
            </button>
          )}
        </div>
      </div>
      {isImage && presignedUrl && (
        <div className="px-2 pb-2">
          <div className="relative group rounded border border-slate-200 overflow-hidden bg-white">
            <Image 
              src={presignedUrl} 
              alt={fileName} 
              width={400}
              height={300}
              unoptimized
              className="w-full h-auto max-h-48 object-contain"
            />
            <div className="absolute inset-0 bg-slate-900/0 group-hover:bg-slate-900/10 transition-colors pointer-events-none" />
          </div>
        </div>
      )}
    </div>
  );
}
