import { useEffect, useState } from "react"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { getSessions } from "@/api/client"
import type { Session } from "@/api/types"
import { cn } from "@/lib/utils"

export interface TimelineFiltersProps {
  sessionFilter: string | undefined
  categoryFilters: Set<string>
  searchText: string
  onSessionFilterChange: (session: string | undefined) => void
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
  categoryFilters,
  searchText,
  onSessionFilterChange,
  onCategoryFilterToggle,
  onSearchChange,
}: TimelineFiltersProps) {
  const [sessions, setSessions] = useState<Session[]>([])

  useEffect(() => {
    getSessions().then(setSessions).catch(console.error)
  }, [])

  return (
    <div className="space-y-3">
      {/* Session and search */}
      <div className="flex gap-2 flex-wrap">
        <Select
          value={sessionFilter || "all"}
          onValueChange={(v) => onSessionFilterChange(v === "all" ? undefined : v)}
        >
          <SelectTrigger className="w-[200px]">
            <SelectValue placeholder="All sessions" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All sessions</SelectItem>
            {sessions.map((s) => (
              <SelectItem key={s.session_id} value={s.session_id}>
                {s.session_name || s.session_id.slice(0, 12)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

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
