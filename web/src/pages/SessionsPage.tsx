import { useCallback, useState } from "react"
import { Input } from "@/components/ui/input"
import { Toggle } from "@/components/ui/toggle"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { SessionCard } from "@/components/SessionCard"
import { InboxDialog } from "@/components/InboxDialog"
import { useSessions, type FilterChip, type SortField } from "@/hooks/useSessions"
import { switchSession } from "@/api/client"
import { toast } from "sonner"
import { cn } from "@/lib/utils"

const filterChips: { key: FilterChip; label: string }[] = [
  { key: "active", label: "Active" },
  { key: "idle", label: "Idle" },
  { key: "dead", label: "Dead" },
  { key: "needs_input", label: "Needs input" },
  { key: "has_unread", label: "Has unread" },
]

interface SessionsPageProps {
  cmuxAvailable: boolean
}

export function SessionsPage({ cmuxAvailable }: SessionsPageProps) {
  const {
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
    refetch,
  } = useSessions()

  const [inboxSession, setInboxSession] = useState<{
    id: string
    name: string
  } | null>(null)

  const handleSwitch = useCallback(
    async (id: string) => {
      try {
        await switchSession(id)
        toast.success("Switched session")
      } catch (e) {
        console.error(e)
        toast.error("Failed to switch")
      }
    },
    []
  )

  const handleInboxOpen = useCallback(
    (id: string) => {
      const all = grouped.flatMap((g) => g.sessions)
      const s = all.find((s) => s.session_id === id)
      setInboxSession({
        id,
        name: s?.session_name || id.slice(0, 12),
      })
    },
    [grouped]
  )

  const totalSessions = grouped.reduce((n, g) => n + g.sessions.length, 0)

  return (
    <div className="space-y-4">
      {/* Top controls */}
      <div className="space-y-3">
        <div className="flex gap-2 flex-wrap">
          <Input
            placeholder="Search sessions..."
            value={search}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) => setSearch(e.target.value)}
            className="flex-1 min-w-[200px]"
          />
          <Toggle
            pressed={groupByRepo}
            onPressedChange={setGroupByRepo}
            variant="outline"
            size="sm"
            className="shrink-0"
          >
            Group
          </Toggle>
          <div className="flex items-center gap-1">
            <Select
              value={sortField}
              onValueChange={(v: string) => setSortField(v as SortField)}
            >
              <SelectTrigger className="w-[140px] h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="cmux">cmux order</SelectItem>
                <SelectItem value="last_prompt">Last prompt</SelectItem>
                <SelectItem value="unread">Unread count</SelectItem>
                <SelectItem value="name">Name</SelectItem>
              </SelectContent>
            </Select>
            <Button
              variant="outline"
              size="sm"
              className="h-9 w-9 p-0"
              onClick={() => setSortReverse((r) => !r)}
              title={sortReverse ? "Reversed" : "Normal order"}
            >
              {sortReverse ? "↑" : "↓"}
            </Button>
          </div>
        </div>

        {/* Filter chips */}
        <div className="flex gap-1.5 overflow-x-auto pb-1">
          {filterChips.map((chip) => (
            <Badge
              key={chip.key}
              variant={filters.has(chip.key) ? "default" : "outline"}
              className={cn(
                "cursor-pointer select-none whitespace-nowrap",
                filters.has(chip.key) && "bg-primary text-primary-foreground"
              )}
              onClick={() => toggleFilter(chip.key)}
            >
              {chip.label}
            </Badge>
          ))}
        </div>
      </div>

      {/* Session list */}
      {loading && (
        <p className="text-sm text-muted-foreground">Loading sessions...</p>
      )}

      {!loading && totalSessions === 0 && (
        <p className="text-sm text-muted-foreground">No sessions match your filters.</p>
      )}

      {groupByRepo
        ? grouped
            .filter((g) => g.sessions.length > 0)
            .map((group, gi) => (
              <div key={gi} className="space-y-2">
                <div className="flex items-center gap-2">
                  {group.workspaceColor && (
                    <div
                      className="w-1 h-5 rounded-full shrink-0"
                      style={{ backgroundColor: group.workspaceColor }}
                    />
                  )}
                  <div className="flex items-center gap-2 text-sm">
                    {group.repo && (
                      <span className="font-semibold text-foreground">
                        {group.repo}
                      </span>
                    )}
                    {group.workspace && (
                      <span className="text-muted-foreground">
                        / {group.workspace}
                      </span>
                    )}
                    {group.branch && (
                      <span className="font-mono text-xs text-muted-foreground">
                        ({group.branch})
                      </span>
                    )}
                  </div>
                </div>
                <div className="space-y-1.5 pl-3">
                  {group.sessions.map((session) => (
                    <SessionCard
                      key={session.session_id}
                      session={session}
                      showBranch={!group.branch}
                      cmuxAvailable={cmuxAvailable}
                      onSwitch={handleSwitch}
                      onInboxOpen={handleInboxOpen}
                    />
                  ))}
                </div>
              </div>
            ))
        : grouped.flatMap((g) =>
            g.sessions.map((session) => (
              <SessionCard
                key={session.session_id}
                session={session}
                showRepoBadge
                cmuxAvailable={cmuxAvailable}
                onSwitch={handleSwitch}
                onInboxOpen={handleInboxOpen}
              />
            ))
          )}

      {/* Inbox dialog */}
      <InboxDialog
        sessionId={inboxSession?.id ?? null}
        sessionName={inboxSession?.name ?? ""}
        cmuxAvailable={cmuxAvailable}
        onClose={() => setInboxSession(null)}
        onRefresh={refetch}
      />
    </div>
  )
}
