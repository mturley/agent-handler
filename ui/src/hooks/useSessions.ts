import { useState, useEffect, useCallback, useMemo } from "react"
import type { Session, DisplayState } from "@/api/types"
import { getSessions } from "@/api/client"
import { useSSE } from "./useSSE"

export type SortField = "cmux" | "last_prompt" | "unread" | "name"

export type FilterChip =
  | "active"
  | "idle"
  | "needs_input"
  | "has_unread"
  | "blocked"

export interface WorkspaceGroup {
  workspace: string
  workspaceColor?: string
  branch?: string
  collapsed: boolean
  sessions: Session[]
}

export interface RepoGroup {
  repo: string
  collapsed: boolean
  workspaces: WorkspaceGroup[]
}

function fuzzyMatch(query: string, session: Session): boolean {
  const q = query.toLowerCase()
  const name = (session.session_name || session.session_id).toLowerCase()
  const branch = (session.branch || "").toLowerCase()
  return name.includes(q) || branch.includes(q)
}

function matchesFilters(session: Session, filters: Set<FilterChip>): boolean {
  if (filters.size === 0) return true

  const stateFilters: DisplayState[] = []
  if (filters.has("active")) stateFilters.push("active")
  if (filters.has("idle")) stateFilters.push("idle")

  // State filters are OR-ed together
  const passesState =
    stateFilters.length === 0 || stateFilters.includes(session.display_state)

  // Property filters are AND-ed
  if (filters.has("needs_input") && !session.needs_input) return false
  if (filters.has("has_unread") && session.unread_count === 0) return false
  // "blocked" — no dedicated field yet, skip for now

  return passesState
}

function sortSessions(a: Session, b: Session, field: SortField, reverse: boolean): number {
  let cmp = 0
  switch (field) {
    case "cmux":
      cmp = a.cmux_order - b.cmux_order
      break
    case "last_prompt":
      cmp = (b.last_prompt || "").localeCompare(a.last_prompt || "")
      break
    case "unread":
      cmp = b.unread_count - a.unread_count
      break
    case "name":
      cmp = (a.session_name || a.session_id).localeCompare(
        b.session_name || b.session_id
      )
      break
  }
  return reverse ? -cmp : cmp
}

function repoName(repo: string): string {
  if (!repo) return "(no repo)"
  // Extract last path component
  const parts = repo.split("/")
  return parts[parts.length - 1] || repo
}

export function useSessions() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [search, setSearch] = useState("")
  const [filters, setFilters] = useState<Set<FilterChip>>(new Set())
  const [sortField, setSortField] = useState<SortField>("cmux")
  const [sortReverse, setSortReverse] = useState(false)
  const [groupByRepo, setGroupByRepo] = useState(true)
  const [loading, setLoading] = useState(true)

  const fetchSessions = useCallback(() => {
    getSessions()
      .then((data) => {
        setSessions(data)
        setLoading(false)
      })
      .catch(console.error)
  }, [])

  useEffect(() => {
    fetchSessions()
  }, [fetchSessions])

  useSSE(fetchSessions)

  const toggleFilter = useCallback((chip: FilterChip) => {
    setFilters((prev) => {
      const next = new Set(prev)
      if (next.has(chip)) {
        next.delete(chip)
      } else {
        next.add(chip)
      }
      return next
    })
  }, [])

  const filtered = useMemo(() => {
    let result = sessions.filter((s) => s.display_state !== "archived")

    if (search) {
      result = result.filter((s) => fuzzyMatch(search, s))
    }

    if (filters.size > 0) {
      result = result.filter((s) => matchesFilters(s, filters))
    }

    result.sort((a, b) => sortSessions(a, b, sortField, sortReverse))

    return result
  }, [sessions, search, filters, sortField, sortReverse])

  const grouped = useMemo((): RepoGroup[] => {
    if (!groupByRepo) {
      // Ungrouped: single repo with single workspace containing all sessions
      return [{
        repo: "",
        collapsed: false,
        workspaces: [{
          workspace: "",
          collapsed: false,
          sessions: filtered
        }]
      }]
    }

    // First, group sessions by repo and workspace
    const repoMap = new Map<string, Map<string, Session[]>>()
    for (const s of filtered) {
      const repo = repoName(s.repo)
      const workspace = s.cmux_workspace || ""

      if (!repoMap.has(repo)) {
        repoMap.set(repo, new Map())
      }
      const workspaceMap = repoMap.get(repo)!
      if (!workspaceMap.has(workspace)) {
        workspaceMap.set(workspace, [])
      }
      workspaceMap.get(workspace)!.push(s)
    }

    // Build nested structure
    const repos: RepoGroup[] = []
    for (const [repo, workspaceMap] of repoMap) {
      const workspaces: WorkspaceGroup[] = []

      for (const [workspace, sessions] of workspaceMap) {
        const workspaceColor = sessions[0]?.cmux_workspace_color
        // If all sessions share the same branch, hoist it to workspace level
        const branches = new Set(sessions.map((s) => s.branch).filter(Boolean))
        const sharedBranch = branches.size === 1 ? [...branches][0] : undefined

        workspaces.push({
          workspace,
          workspaceColor,
          branch: sharedBranch,
          collapsed: false,
          sessions,
        })
      }

      // Sort workspaces by the top session in each workspace
      workspaces.sort((a, b) => {
        if (a.sessions.length === 0 || b.sessions.length === 0) return 0
        return sortSessions(a.sessions[0], b.sessions[0], sortField, sortReverse)
      })

      repos.push({
        repo,
        collapsed: false,
        workspaces,
      })
    }

    // Sort repos by the top session in the top workspace
    repos.sort((a, b) => {
      const aTop = a.workspaces[0]?.sessions[0]
      const bTop = b.workspaces[0]?.sessions[0]
      if (!aTop || !bTop) return 0
      return sortSessions(aTop, bTop, sortField, sortReverse)
    })

    return repos
  }, [filtered, groupByRepo, sortField, sortReverse])

  const allNonArchived = useMemo(
    () => sessions.filter((s) => s.display_state !== "archived"),
    [sessions]
  )

  // Compute counts for each filter chip
  const filterCounts = useMemo(() => {
    const counts: Record<FilterChip, number> = {
      active: 0,
      idle: 0,
      needs_input: 0,
      has_unread: 0,
      blocked: 0,
    }

    for (const s of allNonArchived) {
      if (s.display_state === "active") counts.active++
      if (s.display_state === "idle") counts.idle++
      if (s.needs_input) counts.needs_input++
      if (s.unread_count > 0) counts.has_unread++
    }

    return counts
  }, [allNonArchived])

  const awaitingSessions = useMemo(
    () => allNonArchived
      .filter((s) => s.needs_input)
      .sort((a, b) => (a.session_name || a.session_id).localeCompare(b.session_name || b.session_id)),
    [allNonArchived]
  )

  const unreadSessions = useMemo(
    () => allNonArchived
      .filter((s) => s.unread_count > 0)
      .sort((a, b) => (a.session_name || a.session_id).localeCompare(b.session_name || b.session_id)),
    [allNonArchived]
  )

  return {
    sessions: filtered,
    grouped,
    search,
    setSearch,
    filters,
    toggleFilter,
    filterCounts,
    sortField,
    setSortField,
    sortReverse,
    setSortReverse,
    groupByRepo,
    setGroupByRepo,
    loading,
    refetch: fetchSessions,
    awaitingSessions,
    unreadSessions,
  }
}
