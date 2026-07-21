import { useCallback, useState } from "react"
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

const filterChips: { key: FilterChip; label: string }[] = [
  { key: "active", label: "Active" },
  { key: "idle", label: "Idle" },
  { key: "needs_input", label: "Awaiting approval" },
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
    filterCounts,
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
              {sortReverse ? "↑" : "↓"}
            </Button>
          </div>
        </div>

        {/* Filter chips */}
        <div className="flex gap-1.5 overflow-x-auto pb-1">
          {filterChips.map((chip) => {
            const count = filterCounts[chip.key]
            const isActive = filters.has(chip.key)
            const shouldHighlight =
              !isActive && count > 0 && (chip.key === "needs_input" || chip.key === "has_unread")

            return (
              <Badge
                key={chip.key}
                variant={isActive ? "default" : "outline"}
                className={cn(
                  "cursor-pointer select-none whitespace-nowrap",
                  isActive && "bg-primary text-primary-foreground",
                  shouldHighlight && "bg-amber-100 border-amber-400 text-amber-900 dark:bg-amber-950 dark:border-amber-700 dark:text-amber-200"
                )}
                onClick={() => toggleFilter(chip.key)}
              >
                {chip.label} ({count})
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
        <p className="text-sm text-muted-foreground">No sessions match your filters.</p>
      )}

      {groupByRepo
        ? grouped.map((repo, ri) => {
            const repoKey = `repo:${repo.repo}`
            const repoCollapsed = collapsed.has(repoKey)
            const hasWorkspaces = repo.workspaces.length > 0

            return (
              <div key={ri} className="space-y-2">
                {/* Repo header */}
                {repo.repo && (
                  <div
                    className="flex items-center gap-2 cursor-pointer select-none"
                    onClick={() => toggleRepoCollapse(repo.repo)}
                  >
                    <span className="text-sm text-muted-foreground">
                      {repoCollapsed ? "▸" : "▾"}
                    </span>
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
                      <div key={wi} className="space-y-2">
                        {/* Workspace header with color bar */}
                        <div className="flex items-start gap-2">
                          {workspace.workspaceColor && (
                            <div
                              className="w-1 h-full min-h-[20px] rounded-full shrink-0"
                              style={{ backgroundColor: workspace.workspaceColor }}
                            />
                          )}
                          <div className="flex-1 space-y-2">
                            <div
                              className="flex items-center gap-2 cursor-pointer select-none"
                              onClick={() =>
                                toggleWorkspaceCollapse(repo.repo, workspace.workspace)
                              }
                            >
                              <span className="text-sm text-muted-foreground">
                                {wsCollapsed ? "▸" : "▾"}
                              </span>
                              {workspace.workspace && (
                                <span className="text-sm text-muted-foreground">
                                  {workspace.workspace}
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
          })
        : grouped.flatMap((g) =>
            g.workspaces.flatMap((w) =>
              w.sessions.map((session) => (
                <SessionCard
                  key={session.session_id}
                  session={session}
                  showRepoBadge
                  cmuxAvailable={cmuxAvailable}
                  onSwitch={handleSwitch}
                  onInboxOpen={handleInboxOpen}
                />
              ))
            )
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
