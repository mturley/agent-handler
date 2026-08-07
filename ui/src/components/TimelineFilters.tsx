import { useMemo } from "react"
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
import { getSessions, getArchivedSessions, getResources } from "@/api/client"
import type { Session } from "@/api/types"
import { queryKeys } from "@/api/queryKeys"
import { cn } from "@/lib/utils"

export interface TimelineFiltersProps {
  sessionFilter: string | undefined
  resourceFilter: string | undefined
  includeArchived: boolean
  categoryFilters: Set<string>
  searchText: string
  onSessionFilterChange: (session: string | undefined) => void
  onResourceFilterChange: (resource: string | undefined) => void
  onIncludeArchivedChange: (include: boolean) => void
  onCategoryFilterToggle: (category: string) => void
  onSearchChange: (text: string) => void
  /** Render only the category chips (used on the single-session page). */
  categoriesOnly?: boolean
}

export const CATEGORY_TYPES: Record<string, string[]> = {
  Milestones: ["milestone", "decision"],
  Messages: ["message"],
  Status: ["status", "blocked", "unblocked", "handoff", "followup"],
  CI: ["ci_check_passed", "ci_check_failed", "ci_passed", "ci_failed", "ci_pending", "ci_partial_failure"],
  "PR Activity": ["pr_comment", "pr_review_comment", "pr_approved", "pr_merged", "pr_closed"],
  Jira: ["jira_comment", "jira_status_change", "jira_assigned", "jira_labels_changed", "jira_description_changed"],
}

const CATEGORY_OPTIONS = Object.keys(CATEGORY_TYPES)

function resourceLabel(type: string, id: string): string {
  if (type === "pr") return `PR ${id}`
  return id
}

export function TimelineFilters({
  sessionFilter,
  resourceFilter,
  includeArchived,
  categoryFilters,
  searchText,
  onSessionFilterChange,
  onResourceFilterChange,
  onIncludeArchivedChange,
  onCategoryFilterToggle,
  onSearchChange,
  categoriesOnly,
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

  const { data: resourcesData } = useQuery({
    queryKey: queryKeys.resources,
    queryFn: getResources,
  })

  const allSessions = useMemo(() => {
    const active = activeSessions.map((s) => ({ ...s, _archived: false }))
    const archived = includeArchived && archivedData
      ? archivedData.sessions.map((s) => ({ ...s, _archived: true }))
      : []
    return [...active, ...archived]
  }, [activeSessions, archivedData, includeArchived])

  const resourceOptions = useMemo(() => {
    if (!resourcesData?.resources) return []
    return resourcesData.resources.map((r) => ({
      value: `${r.resource_type}:${r.resource_id}`,
      label: resourceLabel(r.resource_type, r.resource_id),
      title: r.state?.title as string || r.state?.summary as string || undefined,
    }))
  }, [resourcesData])

  return (
    <div className="space-y-3">
      {/* Filters row */}
      {!categoriesOnly && (
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

        <Select
          value={resourceFilter || "all"}
          onValueChange={(v) => onResourceFilterChange(v === "all" ? undefined : v)}
        >
          <SelectTrigger className="w-[200px]">
            <SelectValue placeholder="All resources" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All resources</SelectItem>
            {resourceOptions.map((r) => (
              <SelectItem key={r.value} value={r.value}>
                {r.label}
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
      )}

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
