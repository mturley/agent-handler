import { useState, useCallback, useMemo } from "react"
import { useInfiniteQuery } from "@tanstack/react-query"
import { getArchivedSessions } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"

export type ArchivedSortField = "last_prompt" | "name"

export function useArchivedSessions() {
  const [search, setSearch] = useState("")
  const [sortField, setSortField] = useState<ArchivedSortField>("last_prompt")
  const [sortReverse, setSortReverse] = useState(false)

  const sortParam = sortReverse ? `-${sortField}` : sortField

  const {
    data,
    isLoading: loading,
    isFetchingNextPage: loadingMore,
    hasNextPage: hasMore,
    fetchNextPage,
  } = useInfiniteQuery({
    queryKey: queryKeys.archivedSessions(search || undefined, sortParam),
    queryFn: async ({ pageParam = 0 }: { pageParam?: number }) => {
      return getArchivedSessions({
        limit: 50,
        offset: pageParam,
        search: search || undefined,
        sort: sortParam,
      })
    },
    initialPageParam: 0,
    getNextPageParam: (lastPage, allPages) => {
      const totalFetched = allPages.reduce((n, p) => n + p.sessions.length, 0)
      return lastPage.has_more ? totalFetched : undefined
    },
  })

  const sessions = useMemo(
    () => data?.pages.flatMap((p) => p.sessions) ?? [],
    [data]
  )

  const total = data?.pages[0]?.total ?? 0

  const loadMore = useCallback(() => {
    if (hasMore && !loadingMore) {
      fetchNextPage()
    }
  }, [hasMore, loadingMore, fetchNextPage])

  return {
    sessions,
    total,
    loading,
    loadingMore,
    hasMore: hasMore ?? false,
    loadMore,
    search,
    setSearch,
    sortField,
    setSortField,
    sortReverse,
    setSortReverse,
  }
}
