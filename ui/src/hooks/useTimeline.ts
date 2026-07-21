import { useState, useCallback, useMemo } from "react"
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query"
import { getEvents, type EventsParams } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"

export interface TimelineFilters {
  session?: string
  types?: string[]
  source?: string
  search?: string
}

export function useTimeline() {
  const queryClient = useQueryClient()
  const [filters, setFilters] = useState<TimelineFilters>({})

  const filterKey = useMemo(() => ({
    session: filters.session,
    types: filters.types?.join(","),
    source: filters.source,
    search: filters.search,
  }), [filters])

  const {
    data,
    isLoading: loading,
    isFetchingNextPage: loadingMore,
    hasNextPage: hasMore,
    fetchNextPage,
  } = useInfiniteQuery({
    queryKey: queryKeys.events(filterKey),
    queryFn: async ({ pageParam }: { pageParam?: string }) => {
      const params: EventsParams = { limit: 50 }
      if (pageParam) params.before = pageParam
      if (filters.session) params.session = filters.session
      if (filters.types?.length) params.type = filters.types.join(",")
      if (filters.source) params.source = filters.source
      if (filters.search) params.search = filters.search
      return getEvents(params)
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) =>
      lastPage.has_more ? lastPage.next_cursor : undefined,
  })

  const events = useMemo(
    () => data?.pages.flatMap((p) => p.events) ?? [],
    [data]
  )

  const loadMore = useCallback(() => {
    if (hasMore && !loadingMore) {
      fetchNextPage()
    }
  }, [hasMore, loadingMore, fetchNextPage])

  const updateFilters = useCallback((newFilters: Partial<TimelineFilters>) => {
    setFilters((prev) => ({ ...prev, ...newFilters }))
  }, [])

  const clearFilters = useCallback(() => {
    setFilters({})
  }, [])

  return {
    events,
    loading,
    loadingMore,
    hasMore: hasMore ?? false,
    loadMore,
    updateFilters,
    clearFilters,
  }
}
