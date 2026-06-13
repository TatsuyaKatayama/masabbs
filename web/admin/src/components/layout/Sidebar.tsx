'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { 
  Users, 
  Network, 
  MessageSquare, 
  PlayCircle, 
  Settings,
  Map,
  BarChart3
} from 'lucide-react';

const navigation = [
  { name: 'Agents', href: '/agents', icon: Users },
  { name: 'Org Tree', href: '/org', icon: Network },
  { name: 'Overview', href: '/overview', icon: Map },
  { name: 'Message Board', href: '/board', icon: MessageSquare },
  { name: 'KPI Analytics', href: '/analytics', icon: BarChart3 },
  { name: 'Operations', href: '/operations', icon: PlayCircle },
];

export default function Sidebar() {
  const pathname = usePathname();
  const settingsActive = pathname === '/settings';

  return (
    <div className="flex h-full w-64 flex-col bg-slate-900 text-white">
      <div className="flex h-16 items-center justify-center border-b border-slate-800 bg-slate-950">
        <h1 className="text-xl font-black tracking-wider text-white drop-shadow-md">MASABBS ADMIN</h1>
      </div>
      <nav className="flex-1 space-y-1 px-2 py-4">
        {navigation.map((item) => {
          const isActive = pathname === item.href;
          return (
            <Link
              key={item.name}
              href={item.href}
              className={`group flex items-center rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                isActive 
                  ? 'bg-slate-800 text-indigo-400' 
                  : 'text-slate-300 hover:bg-slate-800 hover:text-white'
              }`}
            >
              <item.icon
                className={`mr-3 h-5 w-5 flex-shrink-0 ${
                  isActive ? 'text-indigo-400' : 'text-slate-400 group-hover:text-white'
                }`}
              />
              {item.name}
            </Link>
          );
        })}
      </nav>
      <div className="border-t border-slate-800 p-4">
        <Link
          href="/settings"
          className={`flex items-center rounded-md px-3 py-2 text-sm font-medium transition-colors ${
            settingsActive
              ? 'bg-slate-800 text-indigo-400'
              : 'text-slate-300 hover:bg-slate-800 hover:text-white'
          }`}
        >
          <Settings className={`mr-3 h-5 w-5 ${settingsActive ? 'text-indigo-400' : 'text-slate-400'}`} />
          Settings
        </Link>
      </div>
    </div>
  );
}
