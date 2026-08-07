import { useState, useCallback } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { getSessionInbox, dismissInbox, dismissEvent, switchSession } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"
import { timeAgo } from "@/utils/timeAgo"
import { formatEventType } from "@/utils/formatLabel"
import { cn } from "@/lib/utils"
import { ChevronRight, ChevronDown, Trash2, Mail, Bell } from "lucide-react"
import { toast } from "sonner"

interface InboxContentProps {
  sessionId: string
  sessionName: string
  cmuxAvailable: boolean
  /** Called after "Dismiss all" succeeds (e.g. to close a wrapping modal). */
  onDismissedAll?: () => void
  /** Tailwind classes for the scroll area (height differs between modal and tab). */
  scrollClassName?: string
  /** Show the "Go to session" switch button in the footer. */
  showSwitch?: boolean
}

/**
 * The inbox event list plus its dismiss controls. Shared by InboxDialog (modal)
 * and the single-session detail page's Inbox tab. Enhancements here appear in
 * both places.
 */
export function InboxContent({
  sessionId,
  sessionName,
  cmuxAvailable,
  onDismissedAll,
  scrollClassName = "max-h-[600px]",
  showSwitch = true,
}: InboxContentProps) {
  const queryClient = useQueryClient()
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [confirmDismiss, setConfirmDismiss] = useState(false)
  const [confirmDismissEvent, setConfirmDismissEvent] = useState<string | null>(null)

  const { data: events = [], isLoading: loading } = useQuery({
    queryKey: queryKeys.inbox(sessionId),
    queryFn: () => getSessionInbox(sessionId),
  })

  const dismissMutation = useMutation({
    mutationFn: () => dismissInbox(sessionId),
    onSuccess: () => {
      toast.success(`Dismissed ${events.length} events`)
      queryClient.invalidateQueries({ queryKey: queryKeys.inbox(sessionId) })
      queryClient.invalidateQueries({ queryKey: ["sessions"] })
      onDismissedAll?.()
    },
    onError: (e) => {
      console.error(e)
      toast.error("Failed to dismiss inbox")
    },
    onSettled: () => setConfirmDismiss(false),
  })

  const dismissEventMutation = useMutation({
    mutationFn: (eventId: string) => dismissEvent(sessionId, eventId),
    onSuccess: () => {
      toast.success("Event dismissed")
      queryClient.invalidateQueries({ queryKey: queryKeys.inbox(sessionId) })
      queryClient.invalidateQueries({ queryKey: ["sessions"] })
    },
    onError: (e) => {
      console.error(e)
      toast.error("Failed to dismiss event")
    },
    onSettled: () => setConfirmDismissEvent(null),
  })

  const toggleExpanded = useCallback((id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const handleSwitch = useCallback(async () => {
    try {
      await switchSession(sessionId)
      toast.success(`Switched to ${sessionName}`)
    } catch (e) {
      console.error(e)
      toast.error("Failed to switch session")
    }
  }, [sessionId, sessionName])

  return (
    <>
      <ScrollArea className={cn(scrollClassName)}>
        {loading && (
          <p className="text-sm text-muted-foreground p-4">Loading...</p>
        )}
        {!loading && events.length === 0 && (
          <p className="text-sm text-muted-foreground p-4">No unread events.</p>
        )}
        {events.map((ev) => {
          const isExpanded = expanded.has(ev.id)
          const confirming = confirmDismissEvent === ev.id
          return (
            <div key={ev.id} className="py-2">
              <div className="flex items-start gap-2">
                <button
                  type="button"
                  className="flex items-start gap-2 flex-1 min-w-0 text-left select-none"
                  onClick={() => ev.body && toggleExpanded(ev.id)}
                >
                  {ev.body ? (
                    isExpanded
                      ? <ChevronDown className="h-4 w-4 mt-0.5 shrink-0 text-muted-foreground" />
                      : <ChevronRight className="h-4 w-4 mt-0.5 shrink-0 text-muted-foreground" />
                  ) : (
                    <span className="w-4 shrink-0" />
                  )}
                  {ev.type === "reminder" ? (
                    <Bell className="h-3.5 w-3.5 mt-0.5 shrink-0 text-purple-400" />
                  ) : (
                    <Mail className="h-3.5 w-3.5 mt-0.5 shrink-0 text-blue-400" />
                  )}
                  <Badge variant="outline" className="text-xs shrink-0">
                    {formatEventType(ev.type)}
                  </Badge>
                  <span className="text-sm flex-1 min-w-0 break-words">{ev.title}</span>
                </button>
                <span className="text-xs text-muted-foreground shrink-0 mt-0.5">
                  {timeAgo(ev.ts)}
                </span>
                {ev.author && (
                  <span className="text-xs text-muted-foreground shrink-0 mt-0.5">
                    {ev.author}
                  </span>
                )}
                {!confirming ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 w-7 p-0 shrink-0"
                    title="Dismiss this event"
                    onClick={() => setConfirmDismissEvent(ev.id)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                ) : (
                  <div className="flex items-center gap-1 shrink-0">
                    <span className="text-xs text-muted-foreground">Dismiss?</span>
                    <Button
                      variant="destructive"
                      size="sm"
                      className="h-7"
                      onClick={() => dismissEventMutation.mutate(ev.id)}
                    >
                      Confirm
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-7"
                      onClick={() => setConfirmDismissEvent(null)}
                    >
                      Cancel
                    </Button>
                  </div>
                )}
              </div>
              {isExpanded && ev.body && (
                <pre className="mt-1 ml-6 text-xs text-muted-foreground whitespace-pre-wrap bg-muted/50 rounded p-2">
                  {ev.body}
                </pre>
              )}
              <Separator className="mt-2" />
            </div>
          )
        })}
      </ScrollArea>

      <div className="flex items-center gap-2 justify-between pt-2">
        {showSwitch && cmuxAvailable && (
          <Button variant="link" size="sm" onClick={handleSwitch}>
            Go to session
          </Button>
        )}
        <div className="flex gap-2 ml-auto">
          {!confirmDismiss ? (
            <Button
              variant="destructive"
              size="sm"
              disabled={events.length === 0}
              onClick={() => setConfirmDismiss(true)}
            >
              Dismiss all
            </Button>
          ) : (
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground">
                Dismiss {events.length} event{events.length !== 1 ? "s" : ""} from {sessionName}?
              </span>
              <Button variant="destructive" size="sm" onClick={() => dismissMutation.mutate()}>
                Confirm
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setConfirmDismiss(false)}
              >
                Cancel
              </Button>
            </div>
          )}
        </div>
      </div>
    </>
  )
}
