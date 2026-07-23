import { useEffect, useRef, useCallback, useState } from "react"
import { TimelineEvent } from "@/components/TimelineEvent"
import { TimelineFilters, CATEGORY_TYPES } from "@/components/TimelineFilters"
import { useTimeline } from "@/hooks/useTimeline"
import { Loader2 } from "lucide-react"

interface TimelinePageProps {
  onSessionClick: (sessionName: string) => void
}

function getUrlParams() {
  const params = new URLSearchParams(window.location.search)
  return {
    session: params.get("session") || undefined,
    archived: params.get("archived") === "true",
  }
}

function updateUrlParams(session?: string, archived?: boolean) {
  const params = new URLSearchParams()
  if (session) params.set("session", session)
  if (archived) params.set("archived", "true")
  const qs = params.toString()
  window.history.replaceState(null, "", qs ? `/timeline?${qs}` : "/timeline")
}

export function TimelinePage({ onSessionClick }: TimelinePageProps) {
  const {
    events,
    loading,
    loadingMore,
    hasMore,
    loadMore,
    updateFilters,
  } = useTimeline()

  const initial = getUrlParams()
  const [sessionFilter, setSessionFilter] = useState<string | undefined>(initial.session)
  const [includeArchived, setIncludeArchived] = useState(initial.archived)
  const [categoryFilters, setCategoryFilters] = useState<Set<string>>(new Set())
  const [searchText, setSearchText] = useState("")

  const handleSessionFilterChange = useCallback((session: string | undefined) => {
    setSessionFilter(session)
    updateUrlParams(session, includeArchived)
  }, [includeArchived])

  const handleIncludeArchivedChange = useCallback((include: boolean) => {
    setIncludeArchived(include)
    updateUrlParams(sessionFilter, include)
  }, [sessionFilter])

  const sentinelRef = useRef<HTMLDivElement>(null)

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
        includeArchived={includeArchived}
        categoryFilters={categoryFilters}
        searchText={searchText}
        onSessionFilterChange={handleSessionFilterChange}
        onIncludeArchivedChange={handleIncludeArchivedChange}
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
          <div className="relative ml-8 space-y-4">
            <div className="absolute -left-[20px] top-0 bottom-0 w-0 border-l-2 border-slate-700" />
            {events.map((event) => (
              <TimelineEvent key={event.id} event={event} onSessionClick={onSessionClick} />
            ))}
          </div>

          {loadingMore && (
            <div className="flex items-center justify-center py-4">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          )}

          {!hasMore && events.length > 0 && (
            <p className="text-sm text-muted-foreground text-center py-4">
              No more events
            </p>
          )}

          <div ref={sentinelRef} className="h-4" />
        </div>
      )}
    </div>
  )
}
