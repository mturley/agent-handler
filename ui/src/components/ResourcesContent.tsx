import { useState } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog"
import { getSessionResources, subscribeResource, unsubscribeResource, type SessionResource } from "@/api/client"
import { formatEventType } from "@/utils/formatLabel"
import { JiraIssueTypeIcon, PRStateIcon } from "@/utils/resourceIcons"
import { ExternalLink, Mail, X, Plus, Loader2 } from "lucide-react"
import { toast } from "sonner"

export function ResourceItem({
  resource,
  confirming,
  onUnwatchClick,
  onUnwatchConfirm,
  onUnwatchCancel,
}: {
  resource: SessionResource
  confirming?: boolean
  onUnwatchClick?: () => void
  onUnwatchConfirm?: () => void
  onUnwatchCancel?: () => void
}) {
  const meta = resource.metadata
  const label = resource.resource_type === "pr"
    ? `PR #${resource.resource_id.split("#")[1] || resource.resource_id}`
    : resource.resource_id
  const hasUnreads = resource.unread_count > 0

  return (
    <div className={cn("py-2 space-y-1 px-2 rounded", hasUnreads && "bg-blue-500/5")}>
      <div className="flex items-start gap-2">
        {resource.resource_type === "jira" && (
          <JiraIssueTypeIcon issueType={meta?.issue_type} className="mt-1" />
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
        {onUnwatchClick && (
          confirming ? (
            <div className="flex items-center gap-1 shrink-0">
              <span className="text-xs text-muted-foreground">Unwatch?</span>
              <Button variant="destructive" size="sm" className="h-7" onClick={onUnwatchConfirm}>
                Confirm
              </Button>
              <Button variant="outline" size="sm" className="h-7" onClick={onUnwatchCancel}>
                Cancel
              </Button>
            </div>
          ) : (
            <Button
              variant="ghost"
              size="sm"
              className="h-7 w-7 p-0 shrink-0"
              title="Unwatch this resource"
              onClick={onUnwatchClick}
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          )
        )}
      </div>
    </div>
  )
}

interface ResourcesContentProps {
  sessionId: string
  scrollClassName?: string
  /** Enable unwatch buttons and the "Watch a resource" control. */
  editable?: boolean
}

/**
 * The per-session watched-resource list, grouped by type. On the session detail
 * page (editable) it also offers per-resource unwatch and a "Watch a resource"
 * modal.
 */
export function ResourcesContent({ sessionId, scrollClassName = "max-h-[400px]", editable }: ResourcesContentProps) {
  const queryClient = useQueryClient()
  const [confirmUnwatch, setConfirmUnwatch] = useState<string | null>(null)
  const [watchOpen, setWatchOpen] = useState(false)
  const [watchInput, setWatchInput] = useState("")
  const [watchError, setWatchError] = useState<string | null>(null)

  const { data: resources = [], isLoading } = useQuery({
    queryKey: ["session-resources", sessionId],
    queryFn: () => getSessionResources(sessionId),
  })

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["session-resources", sessionId] })
    queryClient.invalidateQueries({ queryKey: ["sessions"] })
  }

  const unwatchMutation = useMutation({
    mutationFn: (r: SessionResource) => unsubscribeResource(sessionId, r.resource_type, r.resource_id),
    onSuccess: () => {
      toast.success("Unwatched resource")
      invalidate()
    },
    onError: (e: Error) => toast.error(e.message || "Failed to unwatch"),
    onSettled: () => setConfirmUnwatch(null),
  })

  const watchMutation = useMutation({
    mutationFn: (input: string) => subscribeResource(sessionId, input),
    onSuccess: () => {
      toast.success("Watching resource")
      invalidate()
      setWatchOpen(false)
      setWatchInput("")
      setWatchError(null)
    },
    onError: (e: Error) => setWatchError(e.message || "Failed to watch resource"),
  })

  const sortByUnreads = (a: SessionResource, b: SessionResource) => (b.unread_count || 0) - (a.unread_count || 0)
  const prResources = resources.filter((r) => r.resource_type === "pr").sort(sortByUnreads)
  const jiraResources = resources.filter((r) => r.resource_type === "jira").sort(sortByUnreads)
  const otherResources = resources.filter((r) => r.resource_type !== "pr" && r.resource_type !== "jira").sort(sortByUnreads)

  const key = (r: SessionResource) => `${r.resource_type}:${r.resource_id}`

  const renderGroup = (title: string, list: SessionResource[]) => (
    <div className="px-1">
      <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-1">
        {title} ({list.length})
      </p>
      {list.map((r, i) => (
        <div key={key(r)}>
          <ResourceItem
            resource={r}
            confirming={confirmUnwatch === key(r)}
            onUnwatchClick={editable ? () => setConfirmUnwatch(key(r)) : undefined}
            onUnwatchConfirm={() => unwatchMutation.mutate(r)}
            onUnwatchCancel={() => setConfirmUnwatch(null)}
          />
          {i < list.length - 1 && <Separator />}
        </div>
      ))}
    </div>
  )

  return (
    <div className="space-y-3">
      <ScrollArea className={cn(scrollClassName)}>
        {isLoading && (
          <p className="text-sm text-muted-foreground p-4">Loading...</p>
        )}
        {!isLoading && resources.length === 0 && (
          <p className="text-sm text-muted-foreground p-4">No watched resources.</p>
        )}

        {prResources.length > 0 && renderGroup("Pull Requests", prResources)}
        {prResources.length > 0 && jiraResources.length > 0 && <Separator className="my-2" />}
        {jiraResources.length > 0 && renderGroup("Jira Issues", jiraResources)}
        {otherResources.length > 0 && renderGroup("Other", otherResources)}
      </ScrollArea>

      {editable && (
        <div>
          <Button variant="outline" size="sm" className="gap-1" onClick={() => { setWatchError(null); setWatchOpen(true) }}>
            <Plus className="h-3.5 w-3.5" />
            Watch a resource
          </Button>
        </div>
      )}

      <Dialog open={watchOpen} onOpenChange={(open) => { if (!watchMutation.isPending) setWatchOpen(open) }}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Watch a resource</DialogTitle>
          </DialogHeader>
          <div className="space-y-2">
            <Input
              autoFocus
              placeholder="Paste a GitHub PR or Jira issue link"
              value={watchInput}
              onChange={(e) => { setWatchInput(e.target.value); setWatchError(null) }}
              onKeyDown={(e) => {
                if (e.key === "Enter" && watchInput.trim() && !watchMutation.isPending) {
                  watchMutation.mutate(watchInput.trim())
                }
              }}
            />
            {watchError && (
              <p className="text-xs text-red-400">{watchError}</p>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" size="sm" disabled={watchMutation.isPending} onClick={() => setWatchOpen(false)}>
              Cancel
            </Button>
            <Button
              size="sm"
              disabled={!watchInput.trim() || watchMutation.isPending}
              onClick={() => watchMutation.mutate(watchInput.trim())}
            >
              {watchMutation.isPending && <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" />}
              Watch
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
