import { useCallback, useState, useEffect } from "react"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
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
import { ChevronRight, ChevronDown, ArrowUp, ArrowDown, CircleAlert, Mail, ArrowUpRight } from "lucide-react"
import { Card, CardContent } from "@/components/ui/card"
import { formatEventType } from "@/utils/formatLabel"

const filterChips: { key: FilterChip; label: string }[] = [
  { key: "active", label: "Active" },
  { key: "idle", label: "Idle" },
  { key: "needs_input", label: "Awaiting approval" },
  { key: "has_unread", label: "Has unread" },
]

interface SessionsPageProps {
  cmuxAvailable: boolean
  onTimelineClick: (sessionId: string) => void
}

export function SessionsPage({ cmuxAvailable, onTimelineClick }: SessionsPageProps) {
  const {
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
    refetch,
    awaitingSessions,
    unreadSessions,
  } = useSessions()

  // Apply search query from URL
  useEffect(() => {
    const urlSearch = new URLSearchParams(window.location.search).get("search")
    if (urlSearch) {
      setSearch(urlSearch)
    }
  }, [setSearch])

  const [inboxSession, setInboxSession] = useState<{
    id: string
    name: string
  } | null>(null)

  // Track collapsed state: Set of "repo:repoName" and "ws:repoName:workspaceName"
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())

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
      const all = grouped.flatMap((g) => g.workspaces.flatMap((w) => w.sessions))
      const s = all.find((s) => s.session_id === id)
      setInboxSession({
        id,
        name: s?.session_name || id.slice(0, 12),
      })
    },
    [grouped]
  )

  const totalSessions = grouped.reduce(
    (n, g) => n + g.workspaces.reduce((m, w) => m + w.sessions.length, 0),
    0
  )

  const toggleRepoCollapse = useCallback((repo: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev)
      const key = `repo:${repo}`
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }, [])

  const toggleWorkspaceCollapse = useCallback((repo: string, workspace: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev)
      const key = `ws:${repo}:${workspace}`
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }, [])

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
          <div className="flex items-center gap-2 shrink-0">
            <Switch
              checked={groupByRepo}
              onCheckedChange={setGroupByRepo}
              className="cursor-pointer"
            />
            <label className="text-sm cursor-pointer select-none" onClick={() => setGroupByRepo((prev) => !prev)}>
              Group by repo
            </label>
          </div>
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
              {sortReverse ? <ArrowUp className="h-4 w-4" /> : <ArrowDown className="h-4 w-4" />}
            </Button>
          </div>
        </div>

        {/* Attention summary */}
        {(awaitingSessions.length > 0 || unreadSessions.length > 0) && (
          <Card className="border-amber-500/30 bg-amber-500/5">
            <CardContent className="px-4 py-3 space-y-3">
              {awaitingSessions.length > 0 && (
                <div className="flex items-start gap-2.5">
                  <CircleAlert className="h-5 w-5 text-amber-500 shrink-0 mt-0.5" />
                  <div>
                    <span className="text-base font-bold text-amber-500">
                      {awaitingSessions.length} session{awaitingSessions.length !== 1 ? "s" : ""} awaiting approval
                    </span>
                    <div className="flex flex-wrap gap-x-1 gap-y-1 mt-1">
                      {awaitingSessions.map((s) => (
                        <Button
                          key={s.session_id}
                          variant="outline"
                          size="sm"
                          className="h-7 text-xs"
                          disabled={!cmuxAvailable}
                          onClick={() => handleSwitch(s.session_id)}
                        >
                          {s.session_name || s.session_id.slice(0, 12)}
                          <ArrowUpRight className="h-3.5 w-3.5 ml-1 text-muted-foreground" />
                        </Button>
                      ))}
                    </div>
                  </div>
                </div>
              )}
              {unreadSessions.length > 0 && (
                <div className="flex items-start gap-2.5">
                  <Mail className="h-5 w-5 text-blue-400 shrink-0 mt-0.5" />
                  <div>
                    <span className="text-base font-bold text-blue-400">
                      {unreadSessions.length} session{unreadSessions.length !== 1 ? "s" : ""} with unread messages
                    </span>
                    <div className="flex flex-wrap gap-x-1 gap-y-1 mt-1">
                      {unreadSessions.map((s) => {
                        const breakdown = s.unread_breakdown
                          ? Object.entries(s.unread_breakdown)
                              .sort(([a], [b]) => a.localeCompare(b))
                              .map(([type, count]) => `${count} ${formatEventType(type)}`)
                              .join(", ")
                          : ""
                        return (
                          <Button
                            key={s.session_id}
                            variant="outline"
                            size="sm"
                            className="h-auto py-1 text-xs"
                            disabled={!cmuxAvailable}
                            onClick={() => handleSwitch(s.session_id)}
                          >
                            {s.session_name || s.session_id.slice(0, 12)}
                            {breakdown && (
                              <span className="text-muted-foreground ml-1">({breakdown})</span>
                            )}
                            <ArrowUpRight className="h-3.5 w-3.5 ml-1 text-muted-foreground" />
                          </Button>
                        )
                      })}
                    </div>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        )}

        {/* Filter chips */}
        <div className="flex gap-1.5 overflow-x-auto pb-1">
          {filterChips.map((chip) => {
            const count = filterCounts[chip.key]
            const isActive = filters.has(chip.key)
            const highlightAmber = count > 0 && chip.key === "needs_input"
            const highlightBlue = count > 0 && chip.key === "has_unread"

            return (
              <Badge
                key={chip.key}
                variant={isActive ? "default" : "outline"}
                className={cn(
                  "cursor-pointer select-none whitespace-nowrap gap-1.5 text-sm",
                  isActive && "bg-primary text-primary-foreground",
                )}
                onClick={() => toggleFilter(chip.key)}
              >
                {chip.label}
                {count > 0 && (
                  <span
                    className={cn(
                      "inline-flex items-center justify-center rounded-full text-xs font-bold leading-none min-w-[20px] h-[20px] px-1",
                      highlightAmber
                        ? "bg-amber-500 text-black font-extrabold"
                        : highlightBlue
                        ? "bg-blue-500 text-white font-extrabold"
                        : isActive
                          ? "bg-primary-foreground/20 text-primary-foreground"
                          : "bg-muted text-muted-foreground"
                    )}
                  >
                    {count}
                  </span>
                )}
              </Badge>
            )
          })}
        </div>
      </div>

      {/* Session list */}
      {loading && (
        <p className="text-sm text-muted-foreground">Loading sessions...</p>
      )}

      {!loading && totalSessions === 0 && (
        <p className="text-sm text-muted-foreground py-8 text-center">
          {filters.size > 0 || search
            ? "No sessions match your filters."
            : "No active sessions. Start a Claude Code session to see it here."}
        </p>
      )}

      {grouped.map((repo, ri) => {
        const repoKey = `repo:${repo.repo}`
        const repoCollapsed = collapsed.has(repoKey)
        const hasWorkspaces = repo.workspaces.length > 0

        return (
          <div key={ri} className="space-y-2">
            {/* Repo header (only when grouping by repo) */}
            {repo.repo && (
              <div
                className="flex items-center gap-2 cursor-pointer select-none"
                onClick={() => toggleRepoCollapse(repo.repo)}
              >
                {repoCollapsed
                  ? <ChevronRight className="h-4 w-4 text-muted-foreground" />
                  : <ChevronDown className="h-4 w-4 text-muted-foreground" />
                }
                <span className="font-bold text-foreground">{repo.repo}</span>
              </div>
            )}

            {/* Workspaces */}
            {!repoCollapsed &&
              hasWorkspaces &&
              repo.workspaces.map((workspace, wi) => {
                const wsKey = `ws:${repo.repo}:${workspace.workspace}`
                const wsCollapsed = collapsed.has(wsKey)

                return (
                  <div key={wi} className={cn("space-y-2", repo.repo && "pl-6")}>
                    {/* Workspace header with color bar */}
                    <div className="flex items-stretch gap-2">
                      <div
                        className="w-1 rounded-full shrink-0"
                        style={{ backgroundColor: workspace.workspaceColor || "transparent" }}
                      />
                      <div className="flex-1 space-y-2">
                        <div
                          className="flex items-center gap-2 cursor-pointer select-none"
                          onClick={() =>
                            toggleWorkspaceCollapse(repo.repo, workspace.workspace)
                          }
                        >
                          {wsCollapsed
                            ? <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />
                            : <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
                          }
                          {workspace.workspace && (
                            <span className="text-sm text-muted-foreground">
                              {workspace.workspace}
                            </span>
                          )}
                          <span className="flex-1" />
                          {!groupByRepo && workspace.sessions[0]?.repo && (
                            <span className="font-mono text-xs text-muted-foreground">
                              {workspace.sessions[0].repo.split("/").pop()}
                            </span>
                          )}
                          {workspace.branch && (
                            <span className="font-mono text-xs text-muted-foreground">
                              ({workspace.branch})
                            </span>
                          )}
                        </div>

                        {/* Session cards */}
                        {!wsCollapsed && (
                          <div className="space-y-1.5 pl-3">
                            {workspace.sessions.map((session) => (
                              <SessionCard
                                key={session.session_id}
                                session={session}
                                showBranch={!workspace.branch}
                                cmuxAvailable={cmuxAvailable}
                                onSwitch={handleSwitch}
                                onInboxOpen={handleInboxOpen}
                                onTimelineClick={onTimelineClick}
                              />
                            ))}
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                )
              })}
          </div>
        )
      })}

      {/* Inbox dialog */}
      <InboxDialog
        sessionId={inboxSession?.id ?? null}
        sessionName={inboxSession?.name ?? ""}
        cmuxAvailable={cmuxAvailable}
        onClose={() => setInboxSession(null)}
      />
    </div>
  )
}
