import { useState, useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import type { ResourceEntry, WatcherStatusInfo } from "@/api/types"
import { getResources } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"

export type ResourceSortField = "urgency" | "recent"

interface PRState {
  review_decision?: string
  ci_status?: string
}

interface JiraState {
  blocked?: boolean
  priority?: string
}

function getPRUrgencyScore(state?: Record<string, unknown>): number {
  const prState = state as PRState | undefined
  if (!prState) return 0

  if (prState.ci_status === "FAILURE") return 3
  if (prState.review_decision === "CHANGES_REQUESTED") return 2
  return 1
}

function getJiraUrgencyScore(state?: Record<string, unknown>): number {
  const jiraState = state as JiraState | undefined
  if (!jiraState) return 0

  if (jiraState.blocked === true) return 4
  if (jiraState.priority === "Blocker") return 3
  if (jiraState.priority === "Critical") return 2
  if (jiraState.priority === "Major") return 1
  return 0
}

function sortByUrgency(a: ResourceEntry, b: ResourceEntry): number {
  const aScore = a.resource_type === "pr"
    ? getPRUrgencyScore(a.state)
    : getJiraUrgencyScore(a.state)
  const bScore = b.resource_type === "pr"
    ? getPRUrgencyScore(b.state)
    : getJiraUrgencyScore(b.state)

  if (aScore !== bScore) {
    return bScore - aScore
  }

  const aTime = a.watcher_updated_at || a.resource_updated_at || ""
  const bTime = b.watcher_updated_at || b.resource_updated_at || ""
  return bTime.localeCompare(aTime)
}

function sortByRecent(a: ResourceEntry, b: ResourceEntry): number {
  const aTime = a.watcher_updated_at || a.resource_updated_at || ""
  const bTime = b.watcher_updated_at || b.resource_updated_at || ""
  return bTime.localeCompare(aTime)
}

export function useResources() {
  const [sortField, setSortField] = useState<ResourceSortField>("urgency")

  const { data, isLoading: loading } = useQuery({
    queryKey: queryKeys.resources,
    queryFn: getResources,
    refetchInterval: 10000,
  })

  const resources = data?.resources || []
  const watcherStatus = data?.watcher_status || {}

  const prResources = useMemo(() => {
    const prs = resources.filter((r) => r.resource_type === "pr")
    return sortField === "urgency"
      ? prs.sort(sortByUrgency)
      : prs.sort(sortByRecent)
  }, [resources, sortField])

  const jiraResources = useMemo(() => {
    const jiras = resources.filter((r) => r.resource_type === "jira")
    return sortField === "urgency"
      ? jiras.sort(sortByUrgency)
      : jiras.sort(sortByRecent)
  }, [resources, sortField])

  return {
    resources,
    watcherStatus: watcherStatus as Record<string, WatcherStatusInfo>,
    prResources,
    jiraResources,
    loading,
    sortField,
    setSortField,
  }
}
