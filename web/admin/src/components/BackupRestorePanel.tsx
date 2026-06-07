'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { AlertTriangle, Download, RefreshCw, Save, Trash2, Upload } from 'lucide-react';
import { Config } from '@/types';

type SnapshotKind = 'config' | 'threads' | 'full';

const SNAPSHOT_LABELS: Record<SnapshotKind, { title: string; exportPath: string; importPath: string; fileName: string; warning: string }> = {
  config: {
    title: 'Config Backup',
    exportPath: '/api/v1/configs/export',
    importPath: '/api/v1/configs/import',
    fileName: 'masabbs-config.json',
    warning: 'Config restore replaces teams, agents, memberships, relations and clears thread history.',
  },
  threads: {
    title: 'Thread Backup',
    exportPath: '/api/v1/threads/export',
    importPath: '/api/v1/threads/import',
    fileName: 'masabbs-threads.json',
    warning: 'Thread restore replaces all threads, messages and logs. Matching teams and agents must already exist.',
  },
  full: {
    title: 'Full Backup',
    exportPath: '/api/v1/snapshot/export',
    importPath: '/api/v1/snapshot/import',
    fileName: 'masabbs-full.json',
    warning: 'Full restore replaces saved configs, teams, agents, memberships, relations, threads, messages and logs.',
  },
};

interface BackupRestorePanelProps {
  kinds: SnapshotKind[];
  showPresets?: boolean;
  onRestored?: () => void;
}

export default function BackupRestorePanel({ kinds, showPresets = false, onRestored }: BackupRestorePanelProps) {
  const [configs, setConfigs] = useState<Config[]>([]);
  const [busy, setBusy] = useState(false);
  const importInputRefs = useRef<Record<string, HTMLInputElement | null>>({});

  const refreshConfigs = useCallback(async () => {
    if (!showPresets) return;
    const response = await fetch('/api/v1/configs');
    const data = await response.json();
    if (Array.isArray(data)) setConfigs(data);
  }, [showPresets]);

  useEffect(() => {
    Promise.resolve()
      .then(() => refreshConfigs())
      .catch((err) => console.error('Failed to fetch configs:', err));
  }, [refreshConfigs]);

  const downloadSnapshot = async (kind: SnapshotKind) => {
    const meta = SNAPSHOT_LABELS[kind];
    setBusy(true);
    try {
      const response = await fetch(meta.exportPath);
      if (!response.ok) throw new Error(`Failed to export ${kind}`);
      const blob = await response.blob();
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = meta.fileName;
      link.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Export failed');
    } finally {
      setBusy(false);
    }
  };

  const importSnapshot = async (kind: SnapshotKind, file?: File) => {
    if (!file) return;
    const meta = SNAPSHOT_LABELS[kind];
    if (!confirm(`${meta.warning}\n\nContinue?`)) return;

    setBusy(true);
    try {
      const body = await file.text();
      const response = await fetch(meta.importPath, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body,
      });
      if (!response.ok) {
        const data = await response.json();
        throw new Error(data.error || `Failed to import ${kind}`);
      }
      await refreshConfigs();
      onRestored?.();
      alert('Restore completed.');
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Import failed');
    } finally {
      setBusy(false);
    }
  };

  const createPreset = async () => {
    const name = prompt('Preset name');
    if (!name) return;
    const description = prompt('Description', '') || '';
    setBusy(true);
    try {
      const response = await fetch('/api/v1/configs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, description }),
      });
      if (!response.ok) {
        const data = await response.json();
        throw new Error(data.error || 'Failed to save preset');
      }
      await refreshConfigs();
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Save failed');
    } finally {
      setBusy(false);
    }
  };

  const loadPreset = async (config: Config) => {
    if (!confirm(`Load preset "${config.name}"?\n\nThis replaces teams, agents, relations and clears thread history.`)) return;
    setBusy(true);
    try {
      const response = await fetch(`/api/v1/configs/${config.id}/load`, { method: 'POST' });
      if (!response.ok) {
        const data = await response.json();
        throw new Error(data.error || 'Failed to load preset');
      }
      onRestored?.();
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Load failed');
    } finally {
      setBusy(false);
    }
  };

  const deletePreset = async (config: Config) => {
    if (!confirm(`Delete preset "${config.name}"?`)) return;
    setBusy(true);
    try {
      const response = await fetch(`/api/v1/configs/${config.id}`, { method: 'DELETE' });
      if (!response.ok) throw new Error('Failed to delete preset');
      await refreshConfigs();
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Delete failed');
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="rounded-xl bg-white p-6 shadow-sm ring-1 ring-slate-200">
      <div className="mb-5 flex items-start justify-between border-b border-slate-100 pb-4">
        <div>
          <h2 className="text-lg font-extrabold text-slate-900 tracking-tight">Backup / Restore</h2>
          <p className="mt-1 text-sm text-slate-600">Restore operations are replace, not merge.</p>
        </div>
        {busy && <RefreshCw className="h-4 w-4 animate-spin text-indigo-600" />}
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        {kinds.map((kind) => {
          const meta = SNAPSHOT_LABELS[kind];
          return (
            <div key={kind} className="rounded-xl border border-slate-200 bg-slate-50/50 p-4">
              <div className="mb-3 flex items-center text-xs font-black uppercase tracking-widest text-slate-700">
                <AlertTriangle className="mr-2 h-4 w-4 text-amber-500" />
                {meta.title}
              </div>
              <p className="mb-3 min-h-10 text-[11px] leading-relaxed text-slate-500">{meta.warning}</p>
              <div className="flex gap-2">
                <button onClick={() => downloadSnapshot(kind)} disabled={busy} className="flex items-center rounded-md bg-indigo-600 px-3 py-2 text-xs font-bold text-white shadow-sm hover:bg-indigo-700 disabled:opacity-50">
                  <Download className="mr-1.5 h-3.5 w-3.5" />
                  Export
                </button>
                <button onClick={() => importInputRefs.current[kind]?.click()} disabled={busy} className="flex items-center rounded-md border border-red-200 bg-white px-3 py-2 text-xs font-bold text-red-700 shadow-sm hover:bg-red-50 disabled:opacity-50">
                  <Upload className="mr-1.5 h-3.5 w-3.5" />
                  Restore
                </button>
                <input
                  ref={(node) => { importInputRefs.current[kind] = node; }}
                  type="file"
                  accept="application/json,.json"
                  className="hidden"
                  onChange={(event) => importSnapshot(kind, event.target.files?.[0])}
                />
              </div>
            </div>
          );
        })}
      </div>

      {showPresets && (
        <div className="mt-6 border-t border-slate-100 pt-5">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-xs font-black uppercase tracking-widest text-slate-500">Saved Config Presets</h3>
            <button onClick={createPreset} disabled={busy} className="flex items-center rounded-md bg-indigo-600 px-3 py-2 text-xs font-bold text-white hover:bg-indigo-700 disabled:opacity-50">
              <Save className="mr-1.5 h-3.5 w-3.5" />
              Save Current
            </button>
          </div>
          <div className="space-y-2">
            {configs.map((config) => (
              <div key={config.id} className="flex items-center justify-between rounded-lg border border-slate-100 bg-slate-50 px-3 py-2">
                <div>
                  <div className="text-sm font-bold text-slate-800">{config.name}</div>
                  <div className="text-xs text-slate-500">{config.description || 'No description'}</div>
                </div>
                <div className="flex gap-2">
                  <button onClick={() => loadPreset(config)} disabled={busy} className="rounded-md bg-white px-3 py-1.5 text-xs font-bold text-indigo-700 ring-1 ring-indigo-100 hover:bg-indigo-50 disabled:opacity-50">
                    Load
                  </button>
                  <button onClick={() => deletePreset(config)} disabled={busy} className="rounded-md bg-white p-1.5 text-red-600 ring-1 ring-red-100 hover:bg-red-50 disabled:opacity-50">
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>
              </div>
            ))}
            {configs.length === 0 && <div className="rounded-lg border border-dashed border-slate-200 p-3 text-center text-xs italic text-slate-400">No saved presets.</div>}
          </div>
        </div>
      )}
    </section>
  );
}
