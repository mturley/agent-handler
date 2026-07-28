import { useQuery } from "@tanstack/react-query"
import { useCallback } from "react"
import { cn } from "@/lib/utils"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { getSessionResources, switchSession, type SessionResource } from "@/api/client"
import { formatEventType } from "@/utils/formatLabel"
import { JiraIssueTypeIcon, PRStateIcon } from "@/utils/resourceIcons"
import { ExternalLink, ArrowUpRight, Mail } from "lucide-react"
import { toast } from "sonner"

interface ResourcesDialogProps {
  sessionId: string | null
  sessionName: string
  cmuxAvailable: boolean
  onClose: () => void
}

function ResourceItem({ resource }: { resource: SessionResource }) {
  const meta = resource.metadata
  const label = resource.resource_type === "pr"
    ? `PR #${resource.resource_id.split("#")[1] || resource.resource_id}`
    : resource.resource_id
  const hasUnreads = resource.unread_count > 0

  return (
    <div className={cn("py-2 space-y-1 px-2 rounded", hasUnreads && "bg-blue-500/5")}>
      <div className="flex items-start gap-2">
        {resource.resource_type === "jira" && (
          <JiraIssueTypeIcon issueType={meta?.issue_type} iconUrl={meta?.issue_type_icon} className="mt-1" />
        )}
        {resource.resource_type === "pr" && (
          <PRStateIcon state={meta?.state} className="mt-1" />
        )}
        <div className="flex-1 min-w-0">
          {resource.resource_url ? (
            <a
              href={resource.resource_url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-sm text-blue-400 hover:text-blue-300 hover:underline font-medium"
            >
              {label}
              <ExternalLink className="h-3 w-3 shrink-0" />
            </a>
          ) : (
            <span className="text-sm font-medium">{label}</span>
          )}
          {meta?.title && (
            <p className="text-xs text-muted-foreground mt-0.5">{meta.title}</p>
          )}
          {resource.resource_type === "pr" && meta?.author && (
            <p className="text-xs text-muted-foreground/60">by {meta.author}</p>
          )}
          {resource.resource_type === "jira" && meta && (
            <p className="text-xs text-muted-foreground/60">
              {[meta.status, meta.priority, meta.assignee].filter(Boolean).join(" · ")}
            </p>
          )}
          {hasUnreads && (
            <div className="flex items-center gap-1 text-xs text-blue-400 mt-1">
              <Mail className="h-3 w-3" />
              <span>{resource.unread_count} unread</span>
              {resource.unread_types && (
                <span className="text-blue-400/70">
                  ({Object.entries(resource.unread_types)
                    .sort(([a], [b]) => a.localeCompare(b))
                    .map(([type, count]) => `${count} ${formatEventType(type)}`)
                    .join(", ")})
                </span>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

export function ResourcesDialog({
  sessionId,
  sessionName,
  cmuxAvailable,
  onClose,
}: ResourcesDialogProps) {
  const { data: resources = [], isLoading } = useQuery({
    queryKey: ["session-resources", sessionId],
    queryFn: () => getSessionResources(sessionId!),
    enabled: !!sessionId,
  })

  const sortByUnreads = (a: SessionResource, b: SessionResource) => (b.unread_count || 0) - (a.unread_count || 0)
  const prResources = resources.filter((r) => r.resource_type === "pr").sort(sortByUnreads)
  const jiraResources = resources.filter((r) => r.resource_type === "jira").sort(sortByUnreads)
  const otherResources = resources.filter((r) => r.resource_type !== "pr" && r.resource_type !== "jira").sort(sortByUnreads)

  const handleSwitch = useCallback(async () => {
    if (!sessionId) return
    try {
      await switchSession(sessionId)
      toast.success(`Switched to ${sessionName}`)
    } catch (e) {
      console.error(e)
      toast.error("Failed to switch session")
    }
  }, [sessionId, sessionName])

  return (
    <Dialog open={!!sessionId} onOpenChange={() => onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Resources: {sessionName}</DialogTitle>
          <DialogDescription>
            {resources.length} watched resource{resources.length !== 1 ? "s" : ""}
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className="max-h-[400px]">
          {isLoading && (
            <p className="text-sm text-muted-foreground p-4">Loading...</p>
          )}
          {!isLoading && resources.length === 0 && (
            <p className="text-sm text-muted-foreground p-4">No watched resources.</p>
          )}

          {prResources.length > 0 && (
            <div className="px-1">
              <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-1">
                Pull Requests ({prResources.length})
              </p>
              {prResources.map((r, i) => (
                <div key={i}>
                  <ResourceItem resource={r} />
                  {i < prResources.length - 1 && <Separator />}
                </div>
              ))}
            </div>
          )}

          {prResources.length > 0 && jiraResources.length > 0 && (
            <Separator className="my-2" />
          )}

          {jiraResources.length > 0 && (
            <div className="px-1">
              <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-1">
                Jira Issues ({jiraResources.length})
              </p>
              {jiraResources.map((r, i) => (
                <div key={i}>
                  <ResourceItem resource={r} />
                  {i < jiraResources.length - 1 && <Separator />}
                </div>
              ))}
            </div>
          )}

          {otherResources.length > 0 && (
            <div className="px-1">
              <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-1 mt-2">
                Other ({otherResources.length})
              </p>
              {otherResources.map((r, i) => (
                <div key={i}>
                  <ResourceItem resource={r} />
                  {i < otherResources.length - 1 && <Separator />}
                </div>
              ))}
            </div>
          )}
        </ScrollArea>

        <div className="flex items-center justify-between pt-2">
          {cmuxAvailable && (
            <Button variant="link" size="sm" onClick={handleSwitch}>
              Go to session
              <ArrowUpRight className="h-3 w-3 ml-1" />
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
