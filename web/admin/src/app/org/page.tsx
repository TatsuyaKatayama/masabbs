'use client';

import { Network } from 'lucide-react';

export default function OrgPage() {
  return (
    <div className="h-full flex flex-col items-center justify-center text-slate-400">
      <Network className="h-24 w-24 mb-6 opacity-20" />
      <h1 className="text-2xl font-bold text-slate-900 mb-2">Organization Tree</h1>
      <p className="max-w-md text-center italic">
        Visual representation of agent hierarchies and management relations will be displayed here.
      </p>
      <div className="mt-8 p-6 bg-amber-50 border border-amber-200 rounded-md text-amber-800 text-sm max-w-lg">
        <p className="font-bold mb-2">Development Note:</p>
        <p>This view requires implementing a hierarchical layout (e.g., using D3.js or React Flow) once the <code>agent_relations</code> API endpoints are available.</p>
      </div>
    </div>
  );
}
