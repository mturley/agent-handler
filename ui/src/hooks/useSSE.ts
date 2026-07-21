import { useEffect, useRef } from "react"

export function useSSE(onHeartbeat: () => void, onEventsNew?: () => void) {
  const heartbeatRef = useRef(onHeartbeat)
  heartbeatRef.current = onHeartbeat
  const eventsNewRef = useRef(onEventsNew)
  eventsNewRef.current = onEventsNew

  useEffect(() => {
    let es: EventSource | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null

    function connect() {
      es = new EventSource("/api/stream")

      es.addEventListener("heartbeat", () => {
        heartbeatRef.current()
      })

      es.addEventListener("events_new", () => {
        eventsNewRef.current?.()
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
