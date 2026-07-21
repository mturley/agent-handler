import { useEffect, useState, useCallback } from "react"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import type { Event } from "@/api/types"
import { getSessionInbox, dismissInbox, switchSession } from "@/api/client"
import { timeAgo } from "@/utils/timeAgo"
import { toast } from "sonner"

interface InboxDialogProps {
  sessionId: string | null
  sessionName: string
  cmuxAvailable: boolean
  onClose: () => void
  onRefresh: () => void
}

export function InboxDialog({
  sessionId,
  sessionName,
  cmuxAvailable,
  onClose,
  onRefresh,
}: InboxDialogProps) {
  const [events, setEvents] = useState<Event[]>([])
  const [loading, setLoading] = useState(false)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [confirmDismiss, setConfirmDismiss] = useState(false)

  useEffect(() => {
    if (!sessionId) return
    setLoading(true)
    getSessionInbox(sessionId)
      .then(setEvents)
      .catch((e) => {
        console.error(e)
        toast.error("Failed to load inbox")
      })
      .finally(() => setLoading(false))
  }, [sessionId])

  const toggleExpanded = useCallback((id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const handleDismiss = useCallback(async () => {
    if (!sessionId) return
    try {
      await dismissInbox(sessionId)
      toast.success(`Dismissed ${events.length} events`)
      onRefresh()
      onClose()
    } catch (e) {
      console.error(e)
      toast.error("Failed to dismiss inbox")
    }
    setConfirmDismiss(false)
  }, [sessionId, events.length, onRefresh, onClose])

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
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Inbox: {sessionName}</DialogTitle>
          <DialogDescription>
            {events.length} unread event{events.length !== 1 ? "s" : ""}
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className="max-h-[400px]">
          {loading && (
            <p className="text-sm text-muted-foreground p-4">Loading...</p>
          )}
          {!loading && events.length === 0 && (
            <p className="text-sm text-muted-foreground p-4">No unread events.</p>
          )}
          {events.map((ev) => (
            <div key={ev.id} className="py-2">
              <div
                className="flex items-center gap-2 cursor-pointer select-none"
                onClick={() => toggleExpanded(ev.id)}
              >
                <Badge variant="outline" className="text-xs shrink-0">
                  {ev.type}
                </Badge>
                <span className="text-sm truncate flex-1">{ev.title}</span>
                <span className="text-xs text-muted-foreground shrink-0">
                  {timeAgo(ev.ts)}
                </span>
                {ev.author && (
                  <span className="text-xs text-muted-foreground shrink-0">
                    {ev.author}
                  </span>
                )}
              </div>
              {expanded.has(ev.id) && ev.body && (
                <pre className="mt-1 text-xs text-muted-foreground whitespace-pre-wrap bg-muted/50 rounded p-2">
                  {ev.body}
                </pre>
              )}
              <Separator className="mt-2" />
            </div>
          ))}
        </ScrollArea>

        <div className="flex items-center gap-2 justify-between pt-2">
          {cmuxAvailable && (
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
                <Button variant="destructive" size="sm" onClick={handleDismiss}>
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
      </DialogContent>
    </Dialog>
  )
}
