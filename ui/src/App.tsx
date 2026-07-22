import { useCallback } from "react"
import { useLocation } from "wouter"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Toaster } from "@/components/ui/sonner"
import { useCapabilities } from "@/hooks/useCapabilities"
import { useSSE } from "@/hooks/useSSE"
import { SessionsPage } from "@/pages/SessionsPage"
import { TimelinePage } from "@/pages/TimelinePage"
import { ResourcesPage } from "@/pages/ResourcesPage"

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

  const [location, setLocation] = useLocation()
  const activeTab = pathToTab[location.split("?")[0]] || "sessions"

  const navigateToTimeline = useCallback((sessionId: string) => {
    setLocation(`/timeline?session=${encodeURIComponent(sessionId)}`)
  }, [setLocation])

  const navigateToTimelineByResource = useCallback(
    (resourceType: string, resourceId: string) => {
      setLocation(`/timeline?resource=${encodeURIComponent(`${resourceType}:${resourceId}`)}`)
    },
    [setLocation]
  )

  const navigateToSessions = useCallback((sessionName: string) => {
    setLocation(`/?search=${encodeURIComponent(sessionName)}`)
  }, [setLocation])

  const handleTabChange = useCallback((value: string) => {
    setLocation(tabPaths[value] || "/")
  }, [setLocation])

  return (
    <div className="min-h-screen bg-background">
      <div className="max-w-3xl mx-auto px-4 py-6">
        <header className="mb-6">
          <h1 className="text-lg font-semibold tracking-tight">agent-handler</h1>
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
      <Toaster />
    </div>
  )
}
