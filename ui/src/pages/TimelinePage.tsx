import { useEffect, useRef, useCallback, useState } from "react"
import { useLocation } from "wouter"
import { TimelineEvent } from "@/components/TimelineEvent"
import { TimelineFilters, CATEGORY_TYPES } from "@/components/TimelineFilters"
import { useTimeline } from "@/hooks/useTimeline"
import { Loader2 } from "lucide-react"

interface TimelinePageProps {
  onSessionClick: (sessionName: string) => void
}

export function TimelinePage({ onSessionClick }: TimelinePageProps) {
  const {
    events,
    loading,
    loadingMore,
    hasMore,
    loadMore,
    handleNewEvents,
    updateFilters,
  } = useTimeline()

  const [, setLocation] = useLocation()

  const sessionFilter = new URLSearchParams(window.location.search).get("session") || undefined
  const [categoryFilters, setCategoryFilters] = useState<Set<string>>(new Set())
  const [searchText, setSearchText] = useState("")

  const handleSessionFilterChange = useCallback((session: string | undefined) => {
    if (session) {
      setLocation(`/timeline?session=${encodeURIComponent(session)}`)
    } else {
      setLocation("/timeline")
    }
  }, [setLocation])

  const sentinelRef = useRef<HTMLDivElement>(null)

  // Update filters in the hook when local state changes
  useEffect(() => {
    const selectedTypes = categoryFilters.size > 0
      ? Array.from(categoryFilters).flatMap((cat) => CATEGORY_TYPES[cat] || [])
      : undefined
    updateFilters({
      session: sessionFilter,
      types: selectedTypes,
      search: searchText || undefined,
    })
  }, [sessionFilter, categoryFilters, searchText, updateFilters])

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

  const handleCategoryFilterToggle = useCallback((category: string) => {
    setCategoryFilters((prev) => {
      const next = new Set(prev)
      if (next.has(category)) {
        next.delete(category)
      } else {
        next.add(category)
      }
      return next
    })
  }, [])

  return (
    <div className="space-y-4">
      <TimelineFilters
        sessionFilter={sessionFilter}
        categoryFilters={categoryFilters}
        searchText={searchText}
        onSessionFilterChange={handleSessionFilterChange}
        onCategoryFilterToggle={handleCategoryFilterToggle}
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
        <div>
          {/* Events with timeline line */}
          <div className="relative ml-8 space-y-4">
            <div className="absolute -left-[20px] top-0 bottom-0 w-0 border-l-2 border-slate-700" />
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
