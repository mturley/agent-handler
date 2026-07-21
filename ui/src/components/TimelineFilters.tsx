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
  sourceFilters: Set<string>
  searchText: string
  onSessionFilterChange: (session: string | undefined) => void
  onSourceFilterToggle: (source: string) => void
  onSearchChange: (text: string) => void
}

const SOURCE_OPTIONS = [
  { key: "agent", label: "Agent" },
  { key: "github", label: "GitHub" },
  { key: "jira", label: "Jira" },
]

export function TimelineFilters({
  sessionFilter,
  sourceFilters,
  searchText,
  onSessionFilterChange,
  onSourceFilterToggle,
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

      {/* Source filter chips */}
      <div className="flex gap-1.5 flex-wrap">
        {SOURCE_OPTIONS.map((option) => {
          const isActive = sourceFilters.has(option.key)
          return (
            <Badge
              key={option.key}
              variant={isActive ? "default" : "outline"}
              className={cn(
                "cursor-pointer select-none whitespace-nowrap text-sm",
                isActive && "bg-primary text-primary-foreground"
              )}
              onClick={() => onSourceFilterToggle(option.key)}
            >
              {option.label}
            </Badge>
          )
        })}
      </div>
    </div>
  )
}
