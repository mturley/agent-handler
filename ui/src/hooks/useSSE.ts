import { useEffect } from "react"
import { useQueryClient } from "@tanstack/react-query"

export function useSSE() {
  const queryClient = useQueryClient()

  useEffect(() => {
    let es: EventSource | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null

    function connect() {
      es = new EventSource("/api/stream")

      es.addEventListener("heartbeat", () => {
        queryClient.invalidateQueries({ queryKey: ["sessions"] })
      })

      es.addEventListener("events_new", () => {
        queryClient.invalidateQueries({ queryKey: ["events"] })
        // New events move unread counts on watched resources.
        queryClient.invalidateQueries({ queryKey: ["session-resources"] })
      })

      es.addEventListener("resources_changed", () => {
        queryClient.invalidateQueries({ queryKey: ["session-resources"] })
      })

      // Prefix match: invalidates every session's cron query at once.
      es.addEventListener("crons_changed", () => {
        queryClient.invalidateQueries({ queryKey: ["session-crons"] })
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
