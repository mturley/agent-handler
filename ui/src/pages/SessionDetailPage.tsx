import { useCallback, useState, useEffect } from "react"
import { useLocation } from "wouter"
import { useQuery } from "@tanstack/react-query"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { SessionCard } from "@/components/SessionCard"
import { InboxContent } from "@/components/InboxContent"
import { ResourcesContent } from "@/components/ResourcesContent"
import { AttentionCard } from "@/components/AttentionCard"
import { DailySpendChart } from "@/components/DailySpendChart"
import { useSessions } from "@/hooks/useSessions"
import { TimelinePage } from "@/pages/TimelinePage"
import { getSessions, getArchivedSessions, getEvents, getSessionResources, getSessionCost, switchSession } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"
import { timeAgo } from "@/utils/timeAgo"
import { formatEventType } from "@/utils/formatLabel"
import { ArrowLeft } from "lucide-react"
import { toast } from "sonner"

interface SessionDetailPageProps {
  sessionId: string
  cmuxAvailable: boolean
}

const VALID_TABS = ["timeline", "resources", "cost"] as const

function getTab(): string {
  const t = new URLSearchParams(window.location.search).get("tab")
  return t && (VALID_TABS as readonly string[]).includes(t) ? t : "timeline"
}

export function SessionDetailPage({ sessionId, cmuxAvailable }: SessionDetailPageProps) {
  const [, setLocation] = useLocation()
  const [tab, setTab] = useState(getTab())

  const { data: sessions = [], isLoading: loadingActive } = useQuery({
    queryKey: queryKeys.sessions,
    queryFn: getSessions,
  })
  const { data: archived } = useQuery({
    queryKey: queryKeys.archivedSessions("", "detail"),
    queryFn: () => getArchivedSessions({ limit: 500 }),
  })

  const session =
    sessions.find((s) => s.session_id === sessionId) ||
    archived?.sessions.find((s) => s.session_id === sessionId)

  const name = session?.session_name || sessionId.slice(0, 12)

  // Reflect the focused session in the document title while on this page.
  useEffect(() => {
    document.title = `Handler (${name})`
    return () => { document.title = "Agent Handler" }
  }, [name])

  // Attention summary for OTHER sessions (exclude the one we're viewing).
  const { awaitingSessions, unreadSessions, reminderSessions } = useSessions()
  const otherAwaiting = awaitingSessions.filter((s) => s.session_id !== sessionId)
  const otherUnread = unreadSessions.filter((s) => s.session_id !== sessionId)
  const otherReminders = reminderSessions.filter((s) => s.session_id !== sessionId)

  // Tab badges: last timeline-event time, and per-type resource counts.
  const { data: latestEvents } = useQuery({
    queryKey: ["events", { session: sessionId, limit: 1 }],
    queryFn: () => getEvents({ session: sessionId, limit: 1 }),
  })
  const lastEventTs = latestEvents?.events?.[0]?.ts

  const { data: sessionResources } = useQuery({
    queryKey: ["session-resources", sessionId],
    queryFn: () => getSessionResources(sessionId),
  })
  const resourceCounts = (sessionResources ?? []).reduce<Record<string, number>>((acc, r) => {
    acc[r.resource_type] = (acc[r.resource_type] ?? 0) + 1
    return acc
  }, {})
  const resourceBadge = Object.entries(resourceCounts)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([type, count]) => {
      const label = type === "pr" ? (count === 1 ? "PR" : "PRs") : type === "jira" ? "Jira" : formatEventType(type)
      return `${count} ${label}`
    })
    .join(", ")

  const { data: cost } = useQuery({
    queryKey: ["session-cost", sessionId],
    queryFn: () => getSessionCost(sessionId),
  })
  const costEnabled = cost?.enabled ?? false
  const totalCost = cost?.all_time_cost_usd
  const todayCost = session?.today_cost_usd
  const last30Cost = cost?.total_cost_usd
  const fmtUsd = (v: number) => "$" + v.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })

  const handleTabChange = useCallback((value: string) => {
    setTab(value)
    const params = new URLSearchParams(window.location.search)
    if (value === "timeline") params.delete("tab")
    else params.set("tab", value)
    const qs = params.toString()
    window.history.replaceState(null, "", `/sessions/${sessionId}${qs ? `?${qs}` : ""}`)
  }, [sessionId])

  const handleSwitch = useCallback(async (id: string) => {
    try {
      await switchSession(id)
      toast.success("Switched session")
    } catch {
      toast.error("Failed to switch")
    }
  }, [])

  const noop = useCallback(() => {}, [])

  return (
    <div className="max-w-4xl mx-auto p-4 space-y-4">
      <div className="flex items-center justify-between gap-2">
        <h1 className="text-lg font-semibold tracking-tight truncate">
          <span className="text-muted-foreground font-normal">Session: </span>
          {name}
        </h1>
        <Button variant="ghost" size="sm" className="gap-1 shrink-0" onClick={() => setLocation("/")}>
          <ArrowLeft className="h-4 w-4" />
          All sessions
        </Button>
      </div>

      {!session && !loadingActive ? (
        <p className="text-sm text-muted-foreground py-8 text-center">
          Session not found.
        </p>
      ) : session ? (
        <>
          <AttentionCard
            awaitingSessions={otherAwaiting}
            unreadSessions={otherUnread}
            reminderSessions={otherReminders}
            cmuxAvailable={cmuxAvailable}
            onNavigate={(id) => setLocation(`/sessions/${id}`)}
            onSwitch={handleSwitch}
            other
          />

          <SessionCard
            session={session}
            cmuxAvailable={cmuxAvailable}
            showBranch={false}
            showRepoInfo
            onSwitch={handleSwitch}
            onResourcesOpen={() => handleTabChange("resources")}
            onTimelineClick={noop}
            hideTimelineButton
            expandedInfo
          />

          <InboxContent
            sessionId={sessionId}
            sessionName={name}
            scrollClassName="max-h-[40vh]"
            showHeader
          />

          <Tabs value={tab === "cost" && !costEnabled ? "timeline" : tab} onValueChange={handleTabChange}>
            <TabsList>
              <TabsTrigger value="timeline">
                Timeline
                {lastEventTs && (
                  <Badge variant="secondary" className="ml-1.5 px-1.5 py-0 text-[10px] font-normal">
                    {timeAgo(lastEventTs)}
                  </Badge>
                )}
              </TabsTrigger>
              <TabsTrigger value="resources">
                Resources
                {resourceBadge && (
                  <Badge variant="secondary" className="ml-1.5 px-1.5 py-0 text-[10px] font-normal">
                    {resourceBadge}
                  </Badge>
                )}
              </TabsTrigger>
              {costEnabled && (
                <TabsTrigger value="cost">
                  Cost
                  {totalCost != null && totalCost > 0 && (
                    <Badge variant="secondary" className="ml-1.5 px-1.5 py-0 text-[10px] font-normal">
                      {fmtUsd(totalCost)}
                    </Badge>
                  )}
                </TabsTrigger>
              )}
            </TabsList>

            <TabsContent value="timeline">
              <TimelinePage
                sessionFilter={sessionId}
                hideFilters
                cmuxAvailable={cmuxAvailable}
                onSwitch={handleSwitch}
                onNavigate={(id) => setLocation(`/sessions/${id}`)}
              />
            </TabsContent>

            <TabsContent value="resources">
              <ResourcesContent sessionId={sessionId} scrollClassName="max-h-[60vh]" editable />
            </TabsContent>

            {costEnabled && (
              <TabsContent value="cost">
                <div className="space-y-4">
                  <div className="text-sm space-y-0.5 w-48">
                    <div className="flex justify-between gap-4">
                      <span className="text-muted-foreground">Total</span>
                      <span className="font-medium">{totalCost != null ? fmtUsd(totalCost) : "—"}</span>
                    </div>
                    <div className="flex justify-between gap-4">
                      <span className="text-muted-foreground">Last 30 days</span>
                      <span className="font-medium">{last30Cost != null ? fmtUsd(last30Cost) : "—"}</span>
                    </div>
                    <div className="flex justify-between gap-4">
                      <span className="text-muted-foreground">Today</span>
                      <span className="font-medium">{todayCost != null ? fmtUsd(todayCost) : "$0.00"}</span>
                    </div>
                  </div>
                  <div className="space-y-2">
                    <h3 className="text-sm font-semibold">
                      Daily Spend
                      <span className="text-xs font-normal text-muted-foreground ml-1.5">last 30 days</span>
                    </h3>
                    <DailySpendChart days={cost?.days ?? []} height={120} />
                  </div>
                </div>
              </TabsContent>
            )}
          </Tabs>
        </>
      ) : null}
    </div>
  )
}
