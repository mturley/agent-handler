import { useEffect } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { queryKeys } from "@/api/queryKeys"

export function useSSE() {
  const queryClient = useQueryClient()

  useEffect(() => {
    let es: EventSource | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null

    function connect() {
      es = new EventSource("/api/stream")

      es.addEventListener("heartbeat", () => {
        queryClient.invalidateQueries({ queryKey: queryKeys.sessions })
      })

      es.addEventListener("events_new", () => {
        queryClient.invalidateQueries({ queryKey: ["events"] })
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
  }, [queryClient])
}
