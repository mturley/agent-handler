import { useState, useCallback } from "react"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Toaster } from "@/components/ui/sonner"
import { useCapabilities } from "@/hooks/useCapabilities"
import { SessionsPage } from "@/pages/SessionsPage"
import { TimelinePage } from "@/pages/TimelinePage"

export default function App() {
  const capabilities = useCapabilities()
  const cmuxAvailable = capabilities?.cmux ?? false

  const [activeTab, setActiveTab] = useState("sessions")
  const [timelineSessionFilter, setTimelineSessionFilter] = useState<string | undefined>()
  const [sessionsSearchQuery, setSessionsSearchQuery] = useState<string | undefined>()

  const navigateToTimeline = useCallback((sessionId: string) => {
    setTimelineSessionFilter(sessionId)
    setActiveTab("timeline")
  }, [])

  const navigateToSessions = useCallback((sessionName: string) => {
    setSessionsSearchQuery(sessionName)
    setActiveTab("sessions")
  }, [])

  const handleTabChange = useCallback((value: string) => {
    setActiveTab(value)
    // Clear navigation state when manually switching tabs
    setTimelineSessionFilter(undefined)
    setSessionsSearchQuery(undefined)
  }, [])

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
              searchQuery={sessionsSearchQuery}
            />
          </TabsContent>

          <TabsContent value="timeline">
            <TimelinePage
              onSessionClick={navigateToSessions}
              sessionFilter={timelineSessionFilter}
            />
          </TabsContent>

          <TabsContent value="resources">
            <p className="text-sm text-muted-foreground py-8 text-center">
              Resources view coming soon.
            </p>
          </TabsContent>
        </Tabs>
      </div>
      <Toaster />
    </div>
  )
}
