'use client';

import OrganizationGraph from '@/components/OrganizationGraph';

export default function OrgPage() {
  return (
    <div className="flex h-full w-full flex-col space-y-4">
      <div className="flex items-center justify-between rounded-xl bg-white p-4 shadow-sm ring-1 ring-slate-200">
        <div>
          <h1 className="text-2xl font-extrabold text-slate-900 tracking-tight">Organization Graph</h1>
          <p className="mt-1 text-sm font-medium text-slate-600">Switch teams and edit relationships.</p>
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-hidden rounded-xl bg-white shadow-sm ring-1 ring-slate-200">
        <OrganizationGraph />
      </div>
    </div>
  );
}
