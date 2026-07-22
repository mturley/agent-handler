import { useState, useCallback } from "react"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { WatcherStatus } from "@/components/WatcherStatus"
import { ResourceCard } from "@/components/ResourceCard"
import { useResources, type ResourceSortField } from "@/hooks/useResources"
import { switchSession } from "@/api/client"
import { toast } from "sonner"
import { ChevronRight, ChevronDown } from "lucide-react"

interface ResourcesPageProps {
  cmuxAvailable: boolean
  onTimelineClick: (resourceType: string, resourceId: string) => void
  onSessionClick: (sessionName: string) => void
}

export function ResourcesPage({
  cmuxAvailable,
  onTimelineClick,
  onSessionClick,
}: ResourcesPageProps) {
  const {
    watcherStatus,
    prResources,
    jiraResources,
    sortField,
    setSortField,
  } = useResources()

  const [prCollapsed, setPrCollapsed] = useState(false)
  const [jiraCollapsed, setJiraCollapsed] = useState(false)

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

  return (
    <div className="space-y-4">
      {/* Watcher status bar */}
      <WatcherStatus watcherStatus={watcherStatus} />

      {/* Sort controls */}
      <div className="flex items-center justify-between">
        <h2 className="text-base font-semibold">External Resources</h2>
        <Select
          value={sortField}
          onValueChange={(v: string) => setSortField(v as ResourceSortField)}
        >
          <SelectTrigger className="w-[160px] h-9">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="urgency">Urgency</SelectItem>
            <SelectItem value="recent">Recent activity</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {/* Pull Requests section */}
      {prResources.length > 0 && (
        <div className="space-y-2">
          <div
            className="flex items-center gap-2 cursor-pointer select-none"
            onClick={() => setPrCollapsed(!prCollapsed)}
          >
            {prCollapsed ? (
              <ChevronRight className="h-4 w-4 text-muted-foreground" />
            ) : (
              <ChevronDown className="h-4 w-4 text-muted-foreground" />
            )}
            <span className="font-semibold text-sm">
              Pull Requests ({prResources.length})
            </span>
          </div>

          {!prCollapsed && (
            <div className="space-y-2 pl-6">
              {prResources.map((resource) => (
                <ResourceCard
                  key={`${resource.resource_type}:${resource.resource_id}`}
                  resource={resource}
                  cmuxAvailable={cmuxAvailable}
                  onSwitch={handleSwitch}
                  onTimelineClick={onTimelineClick}
                  onSessionClick={onSessionClick}
                />
              ))}
            </div>
          )}
        </div>
      )}

      {/* Jira Issues section */}
      {jiraResources.length > 0 && (
        <div className="space-y-2">
          <div
            className="flex items-center gap-2 cursor-pointer select-none"
            onClick={() => setJiraCollapsed(!jiraCollapsed)}
          >
            {jiraCollapsed ? (
              <ChevronRight className="h-4 w-4 text-muted-foreground" />
            ) : (
              <ChevronDown className="h-4 w-4 text-muted-foreground" />
            )}
            <span className="font-semibold text-sm">
              Jira Issues ({jiraResources.length})
            </span>
          </div>

          {!jiraCollapsed && (
            <div className="space-y-2 pl-6">
              {jiraResources.map((resource) => (
                <ResourceCard
                  key={`${resource.resource_type}:${resource.resource_id}`}
                  resource={resource}
                  cmuxAvailable={cmuxAvailable}
                  onSwitch={handleSwitch}
                  onTimelineClick={onTimelineClick}
                  onSessionClick={onSessionClick}
                />
              ))}
            </div>
          )}
        </div>
      )}

      {/* Empty state */}
      {prResources.length === 0 && jiraResources.length === 0 && (
        <p className="text-sm text-muted-foreground py-8 text-center">
          No watched resources. Use <code className="text-xs">/watch</code> to subscribe to PRs or Jira issues.
        </p>
      )}
    </div>
  )
}
