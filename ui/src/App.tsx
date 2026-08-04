import { useCallback, useState, useEffect } from "react"
import { useLocation } from "wouter"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Toaster } from "@/components/ui/sonner"
import { useCapabilities } from "@/hooks/useCapabilities"
import { useSSE } from "@/hooks/useSSE"
import { useMediaQuery } from "@/hooks/useMediaQuery"
import { useDynamicFavicon } from "@/hooks/useDynamicFavicon"
import { SessionsPage } from "@/pages/SessionsPage"
import { TimelinePage } from "@/pages/TimelinePage"
import { ResourcesPage } from "@/pages/ResourcesPage"
import { CostBadge, CostDialog } from "@/components/CostDialog"

const tabPaths: Record<string, string> = {
  sessions: "/",
  timeline: "/timeline",
  resources: "/resources",
}

const pathToTab: Record<string, string> = {
  "/": "sessions",
  "/timeline": "timeline",
  "/resources": "resources",
}

export default function App() {
  const capabilities = useCapabilities()
  const cmuxAvailable = capabilities?.cmux ?? false
  useSSE()

  const isWide = useMediaQuery("(min-width: 1024px)")
  const [costOpen, setCostOpen] = useState(false)
  useDynamicFavicon()

  const [location, setLocation] = useLocation()
  const basePath = location.split("?")[0]
  const activeTab = pathToTab[basePath] || "sessions"

  // Right pane tab for wide mode (from URL param, defaults to "timeline")
  const [rightTab, setRightTab] = useState<string>(() => {
    return new URLSearchParams(window.location.search).get("right") || "timeline"
  })
  const [timelineSessionFilter, setTimelineSessionFilter] = useState<string | undefined>()
  const [timelineIncludeArchived, setTimelineIncludeArchived] = useState(false)

  // When switching to wide mode, if we're on timeline/resources, redirect to sessions with right pane
  useEffect(() => {
    if (isWide && (activeTab === "timeline" || activeTab === "resources")) {
      setRightTab(activeTab)
      setLocation(`/?right=${activeTab}`)
    }
  }, [isWide]) // eslint-disable-line react-hooks/exhaustive-deps

  // When switching to narrow mode, if right pane was active, keep sessions selected
  // (user can navigate to timeline/resources via tabs)

  const handleRightTabChange = useCallback((value: string) => {
    setRightTab(value)
    const params = new URLSearchParams(window.location.search)
    params.set("right", value)
    window.history.replaceState(null, "", `/?${params.toString()}`)
  }, [])

  const navigateToTimeline = useCallback((sessionId: string, archived?: boolean) => {
    const params = new URLSearchParams()
    params.set("session", sessionId)
    if (archived) params.set("archived", "true")
    if (isWide) {
      setRightTab("timeline")
      setTimelineSessionFilter((prev) => prev === sessionId ? undefined : sessionId)
      setTimelineIncludeArchived(archived ?? false)
    } else {
      setLocation(`/timeline?${params.toString()}`)
    }
  }, [setLocation, isWide])

  const navigateToTimelineByResource = useCallback(
    (resourceType: string, resourceId: string) => {
      const param = encodeURIComponent(`${resourceType}:${resourceId}`)
      if (isWide) {
        setRightTab("timeline")
        setLocation(`/?right=timeline&resource=${param}`)
      } else {
        setLocation(`/timeline?resource=${param}`)
      }
    },
    [setLocation, isWide]
  )

  const navigateToSessions = useCallback((sessionName: string) => {
    setLocation(`/?search=${encodeURIComponent(sessionName)}`)
  }, [setLocation])

  const handleTabChange = useCallback((value: string) => {
    setLocation(tabPaths[value] || "/")
  }, [setLocation])

  // Wide layout: sessions on left, timeline/resources tabs on right
  if (isWide) {
    return (
      <div className="min-h-screen bg-background">
        <div className="max-w-7xl mx-auto px-4 py-6">
          <header className="mb-6 flex items-center justify-between">
            <h1 className="text-lg font-semibold tracking-tight">agent-handler</h1>
            <CostBadge onClick={() => setCostOpen(true)} />
          </header>

          <div className="flex gap-10">
            {/* Left pane: Sessions */}
            <div className="flex-1 min-w-0">
              <SessionsPage
                cmuxAvailable={cmuxAvailable}
                onTimelineClick={navigateToTimeline}
                activeTimelineSessionId={rightTab === "timeline" ? timelineSessionFilter : undefined}
              />
            </div>

            {/* Right pane: Timeline / Resources */}
            <div className="flex-1 min-w-0">
              <Tabs value={rightTab} onValueChange={handleRightTabChange}>
                <TabsList className="mb-4">
                  <TabsTrigger value="timeline">Timeline</TabsTrigger>
                  <TabsTrigger value="resources">Resources</TabsTrigger>
                </TabsList>

                <TabsContent value="timeline">
                  <TimelinePage
                    onSessionClick={navigateToSessions}
                    sessionFilter={timelineSessionFilter}
                    includeArchived={timelineIncludeArchived}
                  />
                </TabsContent>

                <TabsContent value="resources">
                  <ResourcesPage
                    cmuxAvailable={cmuxAvailable}
                    onTimelineClick={navigateToTimelineByResource}
                    onSessionClick={navigateToSessions}
                  />
                </TabsContent>
              </Tabs>
            </div>
          </div>
        </div>
        <CostDialog open={costOpen} onClose={() => setCostOpen(false)} />
        <Toaster />
      </div>
    )
  }

  // Narrow layout: 3 tabs
  return (
    <div className="min-h-screen bg-background">
      <div className="max-w-3xl mx-auto px-4 py-6">
        <header className="mb-6 flex items-center justify-between">
          <h1 className="text-lg font-semibold tracking-tight">agent-handler</h1>
          <CostBadge onClick={() => setCostOpen(true)} />
        </header>

        <Tabs value={activeTab} onValueChange={handleTabChange}>
          <TabsList className="mb-4">
            <TabsTrigger value="sessions">Sessions</TabsTrigger>
            <TabsTrigger value="timeline">Timeline</TabsTrigger>
            <TabsTrigger value="resources">Resources</TabsTrigger>
          </TabsList>

          <TabsContent value="sessions">
            <SessionsPage
              cmuxAvailable={cmuxAvailable}
              onTimelineClick={navigateToTimeline}
            />
          </TabsContent>

          <TabsContent value="timeline">
            <TimelinePage
              onSessionClick={navigateToSessions}
            />
          </TabsContent>

          <TabsContent value="resources">
            <ResourcesPage
              cmuxAvailable={cmuxAvailable}
              onTimelineClick={navigateToTimelineByResource}
              onSessionClick={navigateToSessions}
            />
          </TabsContent>
        </Tabs>
      </div>
      <CostDialog open={costOpen} onClose={() => setCostOpen(false)} />
      <Toaster />
    </div>
  )
}
