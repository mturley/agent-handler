import { useEffect, useRef, useCallback, useState } from "react"
import { TimelineEvent } from "@/components/TimelineEvent"
import { TimelineFilters } from "@/components/TimelineFilters"
import { useTimeline } from "@/hooks/useTimeline"
import { Loader2 } from "lucide-react"

interface TimelinePageProps {
  onSessionClick: (sessionName: string) => void
  sessionFilter?: string
}

export function TimelinePage({ onSessionClick, sessionFilter: navSessionFilter }: TimelinePageProps) {
  const {
    events,
    loading,
    loadingMore,
    hasMore,
    loadMore,
    handleNewEvents,
    updateFilters,
  } = useTimeline()

  const [sessionFilter, setSessionFilter] = useState<string | undefined>()
  const [sourceFilters, setSourceFilters] = useState<Set<string>>(new Set())
  const [searchText, setSearchText] = useState("")

  // Apply session filter from navigation
  useEffect(() => {
    if (navSessionFilter !== undefined) {
      setSessionFilter(navSessionFilter)
    }
  }, [navSessionFilter])

  const sentinelRef = useRef<HTMLDivElement>(null)

  // Update filters in the hook when local state changes
  useEffect(() => {
    const sourceParam = sourceFilters.size > 0 ? Array.from(sourceFilters).join(",") : undefined
    updateFilters({
      session: sessionFilter,
      source: sourceParam,
      search: searchText || undefined,
    })
  }, [sessionFilter, sourceFilters, searchText, updateFilters])

  // Infinite scroll observer
  useEffect(() => {
    if (!sentinelRef.current) return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting && hasMore && !loadingMore) {
          loadMore()
        }
      },
      { threshold: 0.1 }
    )
    observer.observe(sentinelRef.current)
    return () => observer.disconnect()
  }, [hasMore, loadingMore, loadMore])

  // SSE: handle new events (wired in App.tsx later)
  const handleSSENewEvents = useCallback(() => {
    handleNewEvents()
  }, [handleNewEvents])

  // Expose handleSSENewEvents for SSE integration
  useEffect(() => {
    ;(window as any).__timelineHandleNewEvents = handleSSENewEvents
    return () => {
      delete (window as any).__timelineHandleNewEvents
    }
  }, [handleSSENewEvents])

  const handleSourceFilterToggle = useCallback((source: string) => {
    setSourceFilters((prev) => {
      const next = new Set(prev)
      if (next.has(source)) {
        next.delete(source)
      } else {
        next.add(source)
      }
      return next
    })
  }, [])

  return (
    <div className="space-y-4">
      <TimelineFilters
        sessionFilter={sessionFilter}
        sourceFilters={sourceFilters}
        searchText={searchText}
        onSessionFilterChange={setSessionFilter}
        onSourceFilterToggle={handleSourceFilterToggle}
        onSearchChange={setSearchText}
      />

      {loading && (
        <div className="flex items-center justify-center py-8">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        </div>
      )}

      {!loading && events.length === 0 && (
        <p className="text-sm text-muted-foreground py-8 text-center">
          No events match your filters.
        </p>
      )}

      {!loading && events.length > 0 && (
        <div className="relative">
          {/* Timeline line */}
          <div className="absolute left-3 top-0 bottom-0 w-0 border-l-2 border-slate-700" />

          {/* Events */}
          <div className="ml-6 space-y-4">
            {events.map((event) => (
              <TimelineEvent key={event.id} event={event} onSessionClick={onSessionClick} />
            ))}
          </div>

          {/* Loading more spinner */}
          {loadingMore && (
            <div className="flex items-center justify-center py-4">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          )}

          {/* End marker */}
          {!hasMore && events.length > 0 && (
            <p className="text-sm text-muted-foreground text-center py-4">
              No more events
            </p>
          )}

          {/* Sentinel for infinite scroll */}
          <div ref={sentinelRef} className="h-4" />
        </div>
      )}
    </div>
  )
}
