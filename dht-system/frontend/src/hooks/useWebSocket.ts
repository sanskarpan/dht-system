import { useEffect, useRef, useCallback, useState } from 'react';
import type { DHTEvent, EventType } from '@/types/dht';

interface UseWebSocketOptions {
  url: string;
  onMessage: (event: DHTEvent) => void;
  onConnect?: () => void;
  onDisconnect?: () => void;
}

export function useWebSocket({ url, onMessage, onConnect, onDisconnect }: UseWebSocketOptions) {
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const mountedRef = useRef(true);
  const subscribedTypesRef = useRef<EventType[]>([]);
  const reconnectAttemptRef = useRef(0);
  const [connectionStatus, setConnectionStatus] = useState<'connecting' | 'connected' | 'reconnecting' | 'disconnected'>('connecting');
  const [lastMessageAt, setLastMessageAt] = useState<number | null>(null);

  const connect = useCallback(() => {
    if (!mountedRef.current) return;

    setConnectionStatus(reconnectAttemptRef.current > 0 ? 'reconnecting' : 'connecting');
    const ws = new WebSocket(url);
    wsRef.current = ws;

    ws.onopen = () => {
      if (!mountedRef.current) return;
      reconnectAttemptRef.current = 0;
      setConnectionStatus('connected');
      setLastMessageAt(Date.now());
      onConnect?.();
      // Re-subscribe if we had subscriptions
      if (subscribedTypesRef.current.length > 0) {
        ws.send(JSON.stringify({ action: 'subscribe', events: subscribedTypesRef.current }));
      }
    };

    ws.onmessage = (ev) => {
      if (!mountedRef.current) return;
      setLastMessageAt(Date.now());
      try {
        const event = JSON.parse(ev.data) as DHTEvent;
        onMessage(event);
      } catch {
        // ignore malformed messages
      }
    };

    ws.onclose = () => {
      if (!mountedRef.current) return;
      onDisconnect?.();
      wsRef.current = null;
      setConnectionStatus('reconnecting');
      const attempt = reconnectAttemptRef.current;
      reconnectAttemptRef.current += 1;
      const delay = Math.min(30000, 1000 * (2 ** attempt)) + Math.floor(Math.random() * 250);
      reconnectTimer.current = setTimeout(() => {
        if (mountedRef.current) connect();
      }, delay);
    };

    ws.onerror = () => {
      ws.close();
    };
  }, [url, onMessage, onConnect, onDisconnect]);

  const subscribe = useCallback((types: EventType[]) => {
    subscribedTypesRef.current = types;
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ action: 'subscribe', events: types }));
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    connect();

    return () => {
      mountedRef.current = false;
      if (reconnectTimer.current) clearTimeout(reconnectTimer.current);
      wsRef.current?.close();
    };
  }, [connect]);

  return { subscribe, connectionStatus, lastMessageAt };
}
