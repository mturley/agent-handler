import { useEffect, useRef } from 'react';

export function useSSE(onHeartbeat: () => void) {
  const eventSourceRef = useRef<EventSource | null>(null);

  useEffect(() => {
    let reconnectTimeout: number | undefined;

    function connect() {
      const eventSource = new EventSource('/api/stream');
      eventSourceRef.current = eventSource;

      eventSource.addEventListener('heartbeat', () => {
        onHeartbeat();
      });

      eventSource.onerror = () => {
        eventSource.close();
        // Auto-reconnect after 5 seconds
        reconnectTimeout = window.setTimeout(connect, 5000);
      };
    }

    connect();

    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
      }
      if (reconnectTimeout !== undefined) {
        clearTimeout(reconnectTimeout);
      }
    };
  }, [onHeartbeat]);
}
