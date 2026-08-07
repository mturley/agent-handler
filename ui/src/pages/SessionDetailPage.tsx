import { useCallback, useState } from "react"
import { useLocation } from "wouter"
import { useQuery } from "@tanstack/react-query"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { SessionCard } from "@/components/SessionCard"
import { InboxContent } from "@/components/InboxContent"
import { ResourcesContent } from "@/components/ResourcesContent"
import { TimelinePage } from "@/pages/TimelinePage"
import { getSessions, getArchivedSessions, switchSession } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"
import { ArrowLeft } from "lucide-react"
import { toast } from "sonner"

interface SessionDetailPageProps {
  sessionId: string
  cmuxAvailable: boolean
}

const VALID_TABS = ["inbox", "timeline", "resources"] as const

function getTab(): string {
  const t = new URLSearchParams(window.location.search).get("tab")
  return t && (VALID_TABS as readonly string[]).includes(t) ? t : "inbox"
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

  const handleTabChange = useCallback((value: string) => {
    setTab(value)
    const params = new URLSearchParams(window.location.search)
    if (value === "inbox") params.delete("tab")
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
      <Button variant="ghost" size="sm" className="gap-1" onClick={() => setLocation("/")}>
        <ArrowLeft className="h-4 w-4" />
        All sessions
      </Button>

      {!session && !loadingActive ? (
        <p className="text-sm text-muted-foreground py-8 text-center">
          Session not found.
        </p>
      ) : session ? (
        <>
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

          <Tabs value={tab} onValueChange={handleTabChange}>
            <TabsList>
              <TabsTrigger value="inbox">Inbox</TabsTrigger>
              <TabsTrigger value="timeline">Timeline</TabsTrigger>
              <TabsTrigger value="resources">Resources</TabsTrigger>
            </TabsList>

            <TabsContent value="inbox">
              <InboxContent
                sessionId={sessionId}
                sessionName={name}
                cmuxAvailable={cmuxAvailable}
                scrollClassName="max-h-[60vh]"
                showSwitch={false}
              />
            </TabsContent>

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
              <ResourcesContent sessionId={sessionId} scrollClassName="max-h-[60vh]" />
            </TabsContent>
          </Tabs>
        </>
      ) : null}
    </div>
  )
}
