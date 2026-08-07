import { Card, CardContent } from "@/components/ui/card"
import { SessionLinkButton } from "@/components/PeekPreview"
import { formatEventType } from "@/utils/formatLabel"
import { CircleAlert, Mail, Bell } from "lucide-react"
import type { Session } from "@/api/types"

interface AttentionCardProps {
  awaitingSessions: Session[]
  unreadSessions: Session[]
  reminderSessions: Session[]
  cmuxAvailable: boolean
  onNavigate: (id: string) => void
  onSwitch: (id: string) => void
  /** Frame labels as "N other sessions ..." (used on a session's own detail page). */
  other?: boolean
}

/**
 * The attention summary: sessions awaiting approval, with unread messages, and
 * with reminders, each linking to the session (and switching cmux). Shown on
 * the sessions page and, filtered to "other" sessions, on a session detail page.
 * Renders nothing when all three lists are empty.
 */
export function AttentionCard({
  awaitingSessions,
  unreadSessions,
  reminderSessions,
  cmuxAvailable,
  onNavigate,
  onSwitch,
  other,
}: AttentionCardProps) {
  if (awaitingSessions.length === 0 && unreadSessions.length === 0 && reminderSessions.length === 0) {
    return null
  }

  const label = (n: number, suffix: string) =>
    `${n} ${other ? "other " : ""}session${n !== 1 ? "s" : ""} ${suffix}`

  return (
    <Card className="border-amber-500/30 bg-amber-500/5">
      <CardContent className="px-4 py-3 space-y-3">
        {awaitingSessions.length > 0 && (
          <div className="flex items-start gap-2.5">
            <CircleAlert className="h-5 w-5 text-amber-500 shrink-0 mt-0.5" />
            <div>
              <span className="text-base font-bold text-amber-500">
                {label(awaitingSessions.length, "awaiting approval")}
              </span>
              <div className="flex flex-wrap gap-x-1 gap-y-1 mt-1">
                {awaitingSessions.map((s) => (
                  <SessionLinkButton
                    key={s.session_id}
                    sessionId={s.session_id}
                    sessionName={s.session_name || s.session_id.slice(0, 12)}
                    cmuxAvailable={cmuxAvailable}
                    onNavigate={onNavigate}
                    onSwitch={onSwitch}
                    highlightColor="amber"
                  />
                ))}
              </div>
            </div>
          </div>
        )}
        {unreadSessions.length > 0 && (
          <div className="flex items-start gap-2.5">
            <Mail className="h-5 w-5 text-blue-400 shrink-0 mt-0.5" />
            <div>
              <span className="text-base font-bold text-blue-400">
                {label(unreadSessions.length, "with unread messages")}
              </span>
              <div className="flex flex-wrap gap-x-1 gap-y-1 mt-1">
                {unreadSessions.map((s) => {
                  const breakdown = s.unread_breakdown
                    ? Object.entries(s.unread_breakdown)
                        .filter(([type]) => type !== "reminder")
                        .sort(([a], [b]) => a.localeCompare(b))
                        .map(([type, count]) => `${count} ${formatEventType(type)}`)
                        .join(", ")
                    : ""
                  return (
                    <SessionLinkButton
                      key={s.session_id}
                      sessionId={s.session_id}
                      sessionName={s.session_name || s.session_id.slice(0, 12)}
                      cmuxAvailable={cmuxAvailable}
                      onNavigate={onNavigate}
                      onSwitch={onSwitch}
                      highlightColor="blue"
                      extra={breakdown ? <span className="text-muted-foreground">({breakdown})</span> : undefined}
                    />
                  )
                })}
              </div>
            </div>
          </div>
        )}
        {reminderSessions.length > 0 && (
          <div className="flex items-start gap-2.5">
            <Bell className="h-5 w-5 text-purple-400 shrink-0 mt-0.5" />
            <div>
              <span className="text-base font-bold text-purple-400">
                {label(reminderSessions.length, "with reminders")}
              </span>
              <div className="flex flex-wrap gap-x-1 gap-y-1 mt-1">
                {reminderSessions.map((s) => {
                  const count = s.unread_breakdown?.reminder || s.unread_count
                  return (
                    <SessionLinkButton
                      key={s.session_id}
                      sessionId={s.session_id}
                      sessionName={s.session_name || s.session_id.slice(0, 12)}
                      cmuxAvailable={cmuxAvailable}
                      onNavigate={onNavigate}
                      onSwitch={onSwitch}
                      highlightColor="purple"
                      extra={<span className="text-muted-foreground">({count})</span>}
                    />
                  )
                })}
              </div>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
