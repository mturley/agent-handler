import { useState, useCallback } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Badge } from "@/components/ui/badge"
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
import { getSessionInbox, dismissInbox, dismissEvent, addReminder } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"
import { timeAgo } from "@/utils/timeAgo"
import { formatEventType } from "@/utils/formatLabel"
import { cn } from "@/lib/utils"
import { ChevronRight, ChevronDown, Trash2, Mail, Bell } from "lucide-react"
import { toast } from "sonner"

interface InboxContentProps {
  sessionId: string
  sessionName: string
  /** Called after "Dismiss all" succeeds (e.g. to close a wrapping modal). */
  onDismissedAll?: () => void
  /** Tailwind classes for the scroll area (height differs between modal and tab). */
  scrollClassName?: string
  /** Render an "Inbox" header with the unread count above the list. */
  showHeader?: boolean
}

/**
 * The inbox event list plus its dismiss controls. Shared by InboxDialog (modal)
 * and the single-session detail page's Inbox tab. Enhancements here appear in
 * both places.
 */
export function InboxContent({
  sessionId,
  sessionName,
  onDismissedAll,
  scrollClassName = "max-h-[600px]",
  showHeader = false,
}: InboxContentProps) {
  const queryClient = useQueryClient()
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [confirmDismiss, setConfirmDismiss] = useState(false)
  const [confirmDismissEvent, setConfirmDismissEvent] = useState<string | null>(null)
  const [reminderOpen, setReminderOpen] = useState(false)
  const [reminderText, setReminderText] = useState("")

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

  const addReminderMutation = useMutation({
    mutationFn: (title: string) => addReminder(sessionId, title),
    onSuccess: () => {
      toast.success("Reminder added")
      queryClient.invalidateQueries({ queryKey: queryKeys.inbox(sessionId) })
      queryClient.invalidateQueries({ queryKey: ["sessions"] })
      setReminderOpen(false)
      setReminderText("")
    },
    onError: (e) => {
      console.error(e)
      toast.error("Failed to add reminder")
    },
  })

  const toggleExpanded = useCallback((id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  return (
    <>
      {showHeader && (
        <h2 className="text-base font-semibold mb-1">
          Inbox
          {events.length > 0 && (
            <span className="text-sm font-normal text-muted-foreground ml-1.5">
              {events.length} unread
            </span>
          )}
        </h2>
      )}
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
        <Button
          variant="outline"
          size="sm"
          className="gap-1"
          onClick={() => setReminderOpen(true)}
        >
          <Bell className="h-3.5 w-3.5" />
          Add reminder
        </Button>
        <div className="flex gap-2 ml-auto">
          {events.length === 0 ? null : !confirmDismiss ? (
            <Button
              variant="destructive"
              size="sm"
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

      <Dialog open={reminderOpen} onOpenChange={setReminderOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Add a reminder for {sessionName}</DialogTitle>
          </DialogHeader>
          <Input
            autoFocus
            placeholder="What should this session be reminded of?"
            value={reminderText}
            onChange={(e) => setReminderText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && reminderText.trim()) {
                addReminderMutation.mutate(reminderText.trim())
              }
            }}
          />
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setReminderOpen(false)}>
              Cancel
            </Button>
            <Button
              size="sm"
              disabled={!reminderText.trim() || addReminderMutation.isPending}
              onClick={() => addReminderMutation.mutate(reminderText.trim())}
            >
              Add reminder
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
