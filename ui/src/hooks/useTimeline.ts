import { useState, useCallback, useEffect, useRef } from "react"
import type { TimelineEvent } from "@/api/types"
import { getEvents, type EventsParams } from "@/api/client"

export interface TimelineFilters {
  session?: string
  types?: string[]
  source?: string
  search?: string
}

export function useTimeline() {
  const [events, setEvents] = useState<TimelineEvent[]>([])
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [hasMore, setHasMore] = useState(true)
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [filters, setFilters] = useState<TimelineFilters>({})
  const filtersRef = useRef(filters)
  filtersRef.current = filters

  const buildParams = useCallback((cursor?: string): EventsParams => {
    const params: EventsParams = { limit: 50 }
    if (cursor) params.before = cursor
    if (filtersRef.current.session) params.session = filtersRef.current.session
    if (filtersRef.current.types?.length) params.type = filtersRef.current.types.join(",")
    if (filtersRef.current.source) params.source = filtersRef.current.source
    if (filtersRef.current.search) params.search = filtersRef.current.search
    return params
  }, [])

  // Initial load + filter change reload
  const loadInitial = useCallback(async () => {
    setLoading(true)
    try {
      const data = await getEvents(buildParams())
      setEvents(data.events)
      setHasMore(data.has_more)
      setNextCursor(data.next_cursor || null)
    } catch (err) {
      console.error("Failed to load events:", err)
    } finally {
      setLoading(false)
    }
  }, [buildParams])

  useEffect(() => {
    loadInitial()
  }, [loadInitial, filters])

  // Load more (infinite scroll)
  const loadMore = useCallback(async () => {
    if (loadingMore || !hasMore || !nextCursor) return
    setLoadingMore(true)
    try {
      const data = await getEvents(buildParams(nextCursor))
      setEvents(prev => [...prev, ...data.events])
      setHasMore(data.has_more)
      setNextCursor(data.next_cursor || null)
    } catch (err) {
      console.error("Failed to load more events:", err)
    } finally {
      setLoadingMore(false)
    }
  }, [loadingMore, hasMore, nextCursor, buildParams])

  // SSE: prepend new events
  const handleNewEvents = useCallback(async () => {
    const params = buildParams()
    // Only fetch events newer than the newest we have
    if (events.length > 0) {
      // Fetch without cursor to get latest, then prepend any we don't have
      const data = await getEvents({ ...params, limit: 20 })
      const existingIds = new Set(events.map(e => e.id))
      const newEvents = data.events.filter(e => !existingIds.has(e.id))
      if (newEvents.length > 0) {
        setEvents(prev => [...newEvents, ...prev])
      }
    }
  }, [events, buildParams])

  const updateFilters = useCallback((newFilters: Partial<TimelineFilters>) => {
    setFilters(prev => ({ ...prev, ...newFilters }))
  }, [])

  const clearFilters = useCallback(() => {
    setFilters({})
  }, [])

  return {
    events,
    loading,
    loadingMore,
    hasMore,
    loadMore,
    handleNewEvents,
    filters,
    updateFilters,
    clearFilters,
  }
}
