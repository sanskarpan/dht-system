import { useState, useRef, useEffect } from 'react';
import RingCanvas from './RingCanvas';
import Sidebar from './Sidebar';
import { useDHTStore } from '@/store/dhtStore';
import type { DHTStore } from '@/store/dhtStore';
import { useQuery } from '@tanstack/react-query';
import * as api from '@/api/client';

export default function RingVisualizer() {
  const containerRef = useRef<HTMLDivElement>(null);
  const [dimensions, setDimensions] = useState({ width: 800, height: 600 });
  const [showFingers, setShowFingers] = useState(false);

  const setNetworkState = useDHTStore((s: DHTStore) => s.setNetworkState);
  const connected = useDHTStore((s: DHTStore) => s.connected);
  const nodeCount = useDHTStore((s: DHTStore) => s.nodes.size);
  const keyCount = useDHTStore((s: DHTStore) => s.keys.size);

  // Poll network state as fallback when WebSocket is disconnected
  useQuery({
    queryKey: ['network'],
    queryFn: async () => {
      const state = await api.getNetworkState();
      setNetworkState(state);
      return state;
    },
    refetchInterval: connected ? false : 3000,
  });

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver((entries) => {
      for (const entry of entries) {
        setDimensions({
          width: entry.contentRect.width,
          height: entry.contentRect.height,
        });
      }
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  return (
    <div className="flex h-full min-h-0 flex-col lg:flex-row">
      <Sidebar />
      <div className="flex min-h-0 flex-1 flex-col">
        {/* Status bar */}
        <div className="flex items-center gap-4 px-4 py-2 border-b border-slate-800 text-xs text-slate-400 shrink-0">
          <span>{nodeCount} nodes</span>
          <span>{keyCount} keys</span>
          <label className="flex items-center gap-1.5 cursor-pointer">
            <input
              type="checkbox"
              checked={showFingers}
              onChange={(e) => setShowFingers(e.target.checked)}
              className="accent-blue-500"
            />
            Show finger rays
          </label>
        </div>
        {/* Canvas */}
        <div ref={containerRef} className="flex-1 min-h-0 overflow-hidden">
          {dimensions.width > 0 && (
            <RingCanvas
              width={dimensions.width}
              height={dimensions.height}
              showFingers={showFingers}
            />
          )}
        </div>
      </div>
    </div>
  );
}
