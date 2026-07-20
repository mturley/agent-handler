import { useState, useEffect, useCallback, useMemo } from "react"
import type { Session, DisplayState } from "@/api/types"
import { getSessions } from "@/api/client"
import { useSSE } from "./useSSE"

export type SortField = "cmux" | "last_prompt" | "unread" | "name"

export type FilterChip =
  | "active"
  | "idle"
  | "dead"
  | "needs_input"
  | "has_unread"
  | "blocked"

export interface SessionGroup {
  repo: string
  workspace?: string
  workspaceColor?: string
  branch?: string
  sessions: Session[]
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
  if (filters.has("dead")) stateFilters.push("dead")

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

  const grouped = useMemo((): SessionGroup[] => {
    if (!groupByRepo) return [{ repo: "", sessions: filtered }]

    const map = new Map<string, Session[]>()
    for (const s of filtered) {
      const key = `${repoName(s.repo)}|${s.cmux_workspace || ""}`
      if (!map.has(key)) map.set(key, [])
      map.get(key)!.push(s)
    }

    const groups: SessionGroup[] = []
    for (const [key, sessions] of map) {
      const [repo, workspace] = key.split("|")
      const workspaceColor = sessions[0]?.cmux_workspace_color
      // If all sessions share the same branch, hoist it to group level
      const branches = new Set(sessions.map((s) => s.branch).filter(Boolean))
      const sharedBranch = branches.size === 1 ? [...branches][0] : undefined

      groups.push({
        repo,
        workspace: workspace || undefined,
        workspaceColor,
        branch: sharedBranch,
        sessions,
      })
    }

    // Sort groups by the top session in each group
    groups.sort((a, b) => {
      if (a.sessions.length === 0 || b.sessions.length === 0) return 0
      return sortSessions(a.sessions[0], b.sessions[0], sortField, sortReverse)
    })

    return groups
  }, [filtered, groupByRepo, sortField, sortReverse])

  return {
    sessions: filtered,
    grouped,
    search,
    setSearch,
    filters,
    toggleFilter,
    sortField,
    setSortField,
    sortReverse,
    setSortReverse,
    groupByRepo,
    setGroupByRepo,
    loading,
    refetch: fetchSessions,
  }
}
