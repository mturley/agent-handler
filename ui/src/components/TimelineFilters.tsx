import { useState, useEffect, useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { getSessions, getArchivedSessions } from "@/api/client"
import type { Session } from "@/api/types"
import { queryKeys } from "@/api/queryKeys"
import { cn } from "@/lib/utils"

export interface TimelineFiltersProps {
  sessionFilter: string | undefined
  includeArchived: boolean
  categoryFilters: Set<string>
  searchText: string
  onSessionFilterChange: (session: string | undefined) => void
  onIncludeArchivedChange: (include: boolean) => void
  onCategoryFilterToggle: (category: string) => void
  onSearchChange: (text: string) => void
}

export const CATEGORY_TYPES: Record<string, string[]> = {
  Milestones: ["milestone", "decision"],
  Messages: ["message"],
  Status: ["status", "blocked", "unblocked", "handoff", "followup"],
  CI: ["ci_check_passed", "ci_check_failed"],
  "PR Activity": ["pr_comment", "pr_review_comment", "pr_approved", "pr_merged", "pr_closed"],
  Jira: ["jira_comment", "jira_status_change", "jira_assigned", "jira_labels_changed", "jira_description_changed"],
}

const CATEGORY_OPTIONS = Object.keys(CATEGORY_TYPES)

export function TimelineFilters({
  sessionFilter,
  includeArchived,
  categoryFilters,
  searchText,
  onSessionFilterChange,
  onIncludeArchivedChange,
  onCategoryFilterToggle,
  onSearchChange,
}: TimelineFiltersProps) {
  const { data: activeSessions = [] } = useQuery<Session[]>({
    queryKey: queryKeys.sessions,
    queryFn: getSessions,
  })

  const { data: archivedData } = useQuery({
    queryKey: queryKeys.archivedSessions(),
    queryFn: () => getArchivedSessions({ limit: 200 }),
    enabled: includeArchived,
  })

  const allSessions = useMemo(() => {
    const active = activeSessions.map((s) => ({ ...s, _archived: false }))
    const archived = includeArchived && archivedData
      ? archivedData.sessions.map((s) => ({ ...s, _archived: true }))
      : []
    return [...active, ...archived]
  }, [activeSessions, archivedData, includeArchived])

  return (
    <div className="space-y-3">
      {/* Session dropdown, include archived toggle, and search */}
      <div className="flex gap-2 flex-wrap items-center">
        <Select
          value={sessionFilter || "all"}
          onValueChange={(v) => onSessionFilterChange(v === "all" ? undefined : v)}
        >
          <SelectTrigger className="w-[200px]">
            <SelectValue placeholder="All sessions" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All sessions</SelectItem>
            {allSessions.map((s) => (
              <SelectItem
                key={s.session_id}
                value={s.session_id}
                className={s._archived ? "text-muted-foreground" : ""}
              >
                {s.session_name || s.session_id.slice(0, 12)}
                {s._archived && " (archived)"}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <div className="flex items-center gap-2 shrink-0">
          <Switch
            checked={includeArchived}
            onCheckedChange={onIncludeArchivedChange}
            className="cursor-pointer"
          />
          <label
            className="text-sm cursor-pointer select-none text-muted-foreground"
            onClick={() => onIncludeArchivedChange(!includeArchived)}
          >
            Include archived
          </label>
        </div>

        <Input
          placeholder="Search events..."
          value={searchText}
          onChange={(e) => onSearchChange(e.target.value)}
          className="flex-1 min-w-[200px]"
        />
      </div>

      {/* Category filter chips */}
      <div className="flex gap-1.5 flex-wrap">
        {CATEGORY_OPTIONS.map((category) => {
          const isActive = categoryFilters.has(category)
          return (
            <Badge
              key={category}
              variant={isActive ? "default" : "outline"}
              className={cn(
                "cursor-pointer select-none whitespace-nowrap text-sm",
                isActive && "bg-primary text-primary-foreground"
              )}
              onClick={() => onCategoryFilterToggle(category)}
            >
              {category}
            </Badge>
          )
        })}
      </div>
    </div>
  )
}
