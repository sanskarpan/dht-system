import { BrowserRouter, Routes, Route, NavLink, useNavigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Suspense, lazy, useCallback, useEffect, useState } from 'react';
import { useDHTStore } from '@/store/dhtStore';
import type { DHTStore } from '@/store/dhtStore';
import { useWebSocket } from '@/hooks/useWebSocket';
import type { DHTEvent } from '@/types/dht';
import ToastContainer from '@/components/Toast';
import { toast } from '@/components/toastStore';
import TutorialOverlay from '@/components/TutorialOverlay';
import * as api from '@/api/client';

const RingVisualizer = lazy(() => import('@/components/RingVisualizer'));
const NodeInspector = lazy(() => import('@/components/NodeInspector'));
const LookupTracer = lazy(() => import('@/components/LookupTracer'));
const ConsistencyDash = lazy(() => import('@/components/ConsistencyDash'));
const MetricsPanel = lazy(() => import('@/components/MetricsPanel'));
const ScenarioRunner = lazy(() => import('@/components/ScenarioRunner'));

const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 5000 } },
});

const WS_URL = `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${window.location.host}/ws`;

function AppInner() {
  const applyEvent = useDHTStore((s: DHTStore) => s.applyEvent);
  const setConnected = useDHTStore((s: DHTStore) => s.setConnected);
  const navigate = useNavigate();
  const [showShortcuts, setShowShortcuts] = useState(false);
  const [clockTick, setClockTick] = useState(() => Date.now());

  const handleMessage = useCallback((event: DHTEvent) => {
    applyEvent(event);
  }, [applyEvent]);

  const handleConnect = useCallback(() => {
    setConnected(true);
  }, [setConnected]);

  const handleDisconnect = useCallback(() => {
    setConnected(false);
  }, [setConnected]);

  const { subscribe, connectionStatus, lastMessageAt } = useWebSocket({
    url: WS_URL,
    onMessage: handleMessage,
    onConnect: handleConnect,
    onDisconnect: handleDisconnect,
  });

  useEffect(() => {
    const timer = setInterval(() => setClockTick(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);

  const isStale = connectionStatus === 'connected' && lastMessageAt !== null && clockTick - lastMessageAt > 10000;
  const statusLabel = isStale
    ? 'Stale'
    : connectionStatus === 'connecting'
      ? 'Connecting'
    : connectionStatus === 'reconnecting'
      ? 'Reconnecting'
      : connectionStatus === 'connected'
        ? 'Live'
        : 'Offline';
  const statusClass = isStale
    ? 'border-amber-700/50 bg-amber-900/20 text-amber-400'
    : connectionStatus === 'connecting'
      ? 'border-sky-700/50 bg-sky-900/20 text-sky-400'
    : connectionStatus === 'reconnecting'
      ? 'border-sky-700/50 bg-sky-900/20 text-sky-400'
      : connectionStatus === 'connected'
        ? 'border-emerald-700/50 bg-emerald-900/20 text-emerald-400'
        : 'border-red-800/50 bg-red-900/20 text-red-400';
  const dotClass = isStale
    ? 'bg-amber-400'
    : connectionStatus === 'connecting'
      ? 'bg-sky-400 animate-pulse'
    : connectionStatus === 'reconnecting'
      ? 'bg-sky-400 animate-pulse'
      : connectionStatus === 'connected'
        ? 'bg-emerald-400 animate-conn-pulse'
        : 'bg-red-500';

  useEffect(() => {
    subscribe(['node_join', 'node_leave', 'node_crash', 'stabilize', 'lookup_hop',
               'lookup_complete', 'key_migrate', 'gossip_sync', 'ring_state',
               'conflict_detected', 'write_quorum', 'read_quorum', 'replication_lag']);
  }, [subscribe]);

  // Global keyboard shortcuts (N=add node, K=insert key, L=lookup, ?=help)
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      // Ignore when typing in inputs/textareas
      const tag = (e.target as HTMLElement).tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement).isContentEditable) return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;

      switch (e.key) {
        case 'n':
        case 'N':
          api.spawnNode()
            .then(() => toast.success('Node added to network'))
            .catch((err: unknown) => toast.error(`Failed to add node: ${err instanceof Error ? err.message : String(err)}`));
          break;
        case 'k':
        case 'K':
          navigate('/');
          // Dispatch custom event so Sidebar can focus the insert key input
          window.dispatchEvent(new CustomEvent('dht:focus-key-input'));
          break;
        case 'l':
        case 'L':
          navigate('/lookup');
          window.dispatchEvent(new CustomEvent('dht:focus-lookup-input'));
          break;
        case '?':
          setShowShortcuts((v) => !v);
          break;
        case 'Escape':
          setShowShortcuts(false);
          break;
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [navigate]);

  const navItems = [
    { to: '/', label: 'Ring' },
    { to: '/lookup', label: 'Lookup' },
    { to: '/consistency', label: 'Consistency' },
    { to: '/metrics-ui', label: 'Metrics' },
    { to: '/scenarios', label: 'Scenarios' },
  ];

  return (
    <div className="flex h-screen min-h-0 flex-col text-slate-200" style={{ background: 'var(--color-bg)' }}>
      {/* Top nav */}
      <header className="flex items-center shrink-0 border-b border-slate-800/70"
              style={{ background: 'linear-gradient(180deg, #0d1520 0%, #080e1a 100%)' }}>

        {/* Brand */}
        <div className="flex items-center gap-3 px-5 py-2.5 border-r border-slate-800/60 shrink-0">
          {/* Ring icon */}
          <svg width="30" height="30" viewBox="0 0 30 30" fill="none" aria-hidden="true">
            <circle cx="15" cy="15" r="12" stroke="#1e3a5f" strokeWidth="1.5"/>
            <circle cx="15" cy="15" r="12" stroke="#38bdf8" strokeWidth="1.5"
                    strokeDasharray="5 2.5" strokeLinecap="round" opacity="0.45"/>
            {/* Nodes */}
            <circle cx="15" cy="3.5" r="2.5" fill="#38bdf8"/>
            <circle cx="25.9" cy="21" r="2.5" fill="#10b981"/>
            <circle cx="4.1" cy="21" r="2.5" fill="#f59e0b"/>
            {/* Connections */}
            <line x1="15" y1="3.5" x2="25.9" y2="21" stroke="#38bdf8" strokeWidth="0.8" opacity="0.35"/>
            <line x1="15" y1="3.5" x2="4.1" y2="21" stroke="#38bdf8" strokeWidth="0.8" opacity="0.35"/>
            <line x1="25.9" y1="21" x2="4.1" y2="21" stroke="#10b981" strokeWidth="0.8" opacity="0.3"/>
          </svg>
          <div className="flex flex-col leading-none gap-0.5">
            <span className="text-slate-100 font-bold tracking-[0.15em] text-[13px] uppercase"
                  style={{ fontFamily: 'var(--font-display)' }}>
              DHT<span className="text-sky-400 mx-0.5">·</span>SYS
            </span>
            <span className="text-[9px] tracking-[0.18em] uppercase text-slate-600"
                  style={{ fontFamily: 'var(--font-display)' }}>
              Distributed Hash Table
            </span>
          </div>
        </div>

        {/* Nav */}
        <nav className="flex h-full min-w-0 flex-1 overflow-x-auto px-2">
          {navItems.map(({ to, label }) => (
            <NavLink
              key={to}
              to={to}
              end={to === '/'}
              className={({ isActive }) =>
                `relative flex shrink-0 items-center whitespace-nowrap px-4 py-3 text-xs font-semibold tracking-[0.1em] uppercase transition-colors ${
                  isActive
                    ? 'text-sky-300'
                    : 'text-slate-500 hover:text-slate-300'
                }`
              }
            >
              {({ isActive }) => (
                <>
                  {label}
                  {isActive && (
                    <span className="absolute bottom-0 left-3 right-3 h-px bg-sky-400 rounded-full" />
                  )}
                </>
              )}
            </NavLink>
          ))}
        </nav>

        {/* Right section */}
        <div className="ml-auto flex items-center gap-3 px-5">
          <button
            onClick={() => setShowShortcuts((v) => !v)}
            className="text-[10px] text-slate-600 hover:text-slate-400 border border-slate-700/60 hover:border-slate-600 rounded px-1.5 py-0.5 transition-colors"
            style={{ fontFamily: 'var(--font-mono)' }}
            title="Keyboard shortcuts (?)"
          >
            ?
          </button>
          <div className={`flex items-center gap-1.5 px-2.5 py-1 rounded-full border text-[10px] font-semibold tracking-wider uppercase transition-all ${statusClass}`}>
            <span className={`w-1.5 h-1.5 rounded-full ${dotClass}`} />
            {statusLabel}
          </div>
        </div>
      </header>

      {/* Keyboard shortcuts modal */}
      {showShortcuts && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm"
          onClick={() => setShowShortcuts(false)}
        >
          <div
            className="border border-slate-700/80 rounded-lg p-6 w-72 shadow-2xl animate-fade-in"
            style={{ background: 'linear-gradient(135deg, #0d1520 0%, #080e1a 100%)' }}
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center gap-2 mb-5">
              <span className="w-1 h-4 bg-sky-400 rounded-full" />
              <h3 className="text-slate-100 font-semibold text-sm tracking-wide uppercase">
                Keyboard Shortcuts
              </h3>
            </div>
            <table className="w-full">
              <tbody>
                {[
                  ['N', 'Add a new node'],
                  ['K', 'Focus key insert form'],
                  ['L', 'Open lookup tracer'],
                  ['?', 'Toggle this help'],
                  ['Esc', 'Close modal'],
                ].map(([key, desc]) => (
                  <tr key={key} className="border-b border-slate-800/60 last:border-0">
                    <td className="py-2.5 pr-4 w-12">
                      <kbd className="text-sky-300 border border-sky-800/60 bg-sky-900/20 rounded px-2 py-0.5 text-xs"
                           style={{ fontFamily: 'var(--font-mono)' }}>
                        {key}
                      </kbd>
                    </td>
                    <td className="py-2.5 text-slate-400 text-xs">{desc}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            <button
              onClick={() => setShowShortcuts(false)}
              className="mt-5 w-full text-xs text-slate-600 hover:text-slate-400 py-1.5 border border-slate-800/60 hover:border-slate-700 rounded transition-colors"
            >
              Close
            </button>
          </div>
        </div>
      )}

      {/* Main content */}
      <main className="flex-1 overflow-hidden" style={{ background: 'var(--color-bg)' }}>
        <Suspense
          fallback={
            <div className="flex h-full items-center justify-center text-xs uppercase tracking-[0.18em] text-slate-500">
              Loading View
            </div>
          }
        >
          <Routes>
            <Route path="/" element={<RingVisualizer />} />
            <Route path="/lookup" element={<LookupTracer />} />
            <Route path="/nodes/:id" element={<NodeInspector />} />
            <Route path="/consistency" element={<ConsistencyDash />} />
            <Route path="/metrics-ui" element={<MetricsPanel />} />
            <Route path="/scenarios" element={<ScenarioRunner />} />
          </Routes>
        </Suspense>
      </main>

      {/* Guided tour for first-time users */}
      <TutorialOverlay />

      {/* Global toast notifications */}
      <ToastContainer />
    </div>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AppInner />
      </BrowserRouter>
    </QueryClientProvider>
  );
}
