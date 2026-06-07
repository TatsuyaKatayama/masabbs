'use client';

import BackupRestorePanel from '@/components/BackupRestorePanel';
import { Database, MessageSquare, Settings as SettingsIcon } from 'lucide-react';

export default function SettingsPage() {
  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-extrabold text-slate-900 tracking-tight">Settings</h1>
        <p className="mt-2 text-lg text-slate-700 font-medium">Manage backup, restore, and saved presets.</p>
      </header>

      <section className="grid grid-cols-1 gap-6 xl:grid-cols-3">
        <div className="rounded-xl bg-white p-6 shadow-sm ring-1 ring-slate-200">
          <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-lg bg-indigo-50 text-indigo-600">
            <SettingsIcon className="h-5 w-5" />
          </div>
          <h2 className="text-sm font-black uppercase tracking-widest text-slate-900">Config Restore</h2>
          <p className="mt-2 text-sm leading-relaxed text-slate-600">
            Replaces teams, agents, memberships, and relations. Thread history is cleared.
          </p>
        </div>

        <div className="rounded-xl bg-white p-6 shadow-sm ring-1 ring-slate-200">
          <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-lg bg-indigo-50 text-indigo-600">
            <MessageSquare className="h-5 w-5" />
          </div>
          <h2 className="text-sm font-black uppercase tracking-widest text-slate-900">Thread Restore</h2>
          <p className="mt-2 text-sm leading-relaxed text-slate-600">
            Replaces threads, messages, and logs. Matching teams and agents must already exist.
          </p>
        </div>

        <div className="rounded-xl bg-white p-6 shadow-sm ring-1 ring-slate-200">
          <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-lg bg-indigo-50 text-indigo-600">
            <Database className="h-5 w-5" />
          </div>
          <h2 className="text-sm font-black uppercase tracking-widest text-slate-900">Full Restore</h2>
          <p className="mt-2 text-sm leading-relaxed text-slate-600">
            Replaces saved configs, organization data, thread history, messages, and logs.
          </p>
        </div>
      </section>

      <BackupRestorePanel kinds={['config', 'threads', 'full']} showPresets onRestored={() => window.location.reload()} />
    </div>
  );
}

