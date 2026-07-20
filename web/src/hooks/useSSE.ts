import { useEffect, useRef } from "react"

export function useSSE(onHeartbeat: () => void) {
  const callbackRef = useRef(onHeartbeat)
  callbackRef.current = onHeartbeat

  useEffect(() => {
    let es: EventSource | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null

    function connect() {
      es = new EventSource("/api/stream")

      es.addEventListener("heartbeat", () => {
        callbackRef.current()
      })

      es.onerror = () => {
        es?.close()
        es = null
        reconnectTimer = setTimeout(connect, 3000)
      }
    }

    connect()

    return () => {
      es?.close()
      if (reconnectTimer) clearTimeout(reconnectTimer)
    }
  }, [])
}
