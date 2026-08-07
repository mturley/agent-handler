import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import type { Session } from "@/api/types"
import { timeAgo } from "@/utils/timeAgo"
import { cn } from "@/lib/utils"
import { CircleAlert, Terminal, Mail, List, OctagonAlert, Asterisk, Bell } from "lucide-react"
import { PeekSwitchButton } from "@/components/PeekPreview"
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip"
import { formatEventType } from "@/utils/formatLabel"
import { ContextBar, SessionInfoDetails, hasSessionInfo } from "@/components/SessionInfo"

const stateColors: Record<string, string> = {
  active: "bg-green-500",
  idle: "bg-amber-500",
  dead: "bg-red-500",
  archived: "bg-slate-500",
}

const stateLabels: Record<string, string> = {
  active: "Active",
  idle: "Idle",
  dead: "Dead",
  archived: "Archived",
}

const PR_EVENT_TYPES = new Set(["pr_comment", "pr_review_comment", "pr_approved", "pr_merged", "pr_closed", "ci_check_passed", "ci_check_failed"])
const JIRA_EVENT_TYPES = new Set(["jira_comment", "jira_status_change", "jira_assigned", "jira_labels_changed", "jira_description_changed"])

function hasUnreadsForResourceType(resourceType: string, breakdown: Record<string, number>): boolean {
  const eventTypes = resourceType === "pr" ? PR_EVENT_TYPES : resourceType === "jira" ? JIRA_EVENT_TYPES : null
  if (!eventTypes) return false
  return Object.keys(breakdown).some((t) => eventTypes.has(t))
}

interface SessionCardProps {
  session: Session
  showBranch?: boolean
  showRepoInfo?: boolean
  cmuxAvailable: boolean
  isTimelineActive?: boolean
  onSwitch: (id: string) => void
  onResourcesOpen: (id: string) => void
  onTimelineClick: (id: string) => void
  /** When set, clicking the card body (not its inner controls) navigates. */
  onCardClick?: (id: string) => void
  /** Hide the "view timeline" button (e.g. on the single-session page). */
  hideTimelineButton?: boolean
  /** Show model/context/cost details inline under the context bar (detail page). */
  expandedInfo?: boolean
}

export function SessionCard({
  session,
  showBranch = true,
  showRepoInfo,
  cmuxAvailable,
  isTimelineActive,
  onSwitch,
  onResourcesOpen,
  onTimelineClick,
  onCardClick,
  hideTimelineButton,
  expandedInfo,
}: SessionCardProps) {
  const name = session.session_name || session.session_id.slice(0, 12)

  // Split unread into non-reminder events (blue) and reminders (purple).
  const breakdown = session.unread_breakdown ?? {}
  const reminderCount = breakdown["reminder"] ?? 0
  const nonReminderEntries = Object.entries(breakdown)
    .filter(([type]) => type !== "reminder")
    .sort(([a], [b]) => a.localeCompare(b))
  const nonReminderCount = session.unread_count - reminderCount
  const nonReminderTypes = nonReminderEntries
    .map(([type, count]) => `${count} ${formatEventType(type)}`)
    .join(", ")

  const card = (
    <Card
      onClick={onCardClick ? () => onCardClick(session.session_id) : undefined}
      className={cn(
        "transition-colors",
        onCardClick && "cursor-pointer hover:bg-accent/40",
        session.needs_input && "border-2 border-amber-500/50 bg-amber-950/20",
        session.blocked && !session.needs_input && "border-2 border-red-500/50 bg-red-950/20",
        session.unread_count > 0 && !session.needs_input && !session.blocked && "border-2 border-blue-500/50 bg-blue-950/20"
      )}
    >
      <CardHeader className="pb-2 pt-3 px-4">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0">
            {session.working ? (
              <Asterisk className="h-3.5 w-3.5 animate-pulse-fast text-orange-400 shrink-0" />
            ) : (
              <div
                className={cn("w-2 h-2 rounded-full shrink-0", stateColors[session.display_state])}
              />
            )}
            <span className="font-semibold text-sm truncate">{name}</span>
            {session.needs_input && (
              <span className="inline-flex items-center gap-1 text-amber-500 shrink-0">
                <CircleAlert className="h-4 w-4" />
                <span className="text-xs font-medium">Awaiting approval</span>
              </span>
            )}
            <span className="text-xs text-muted-foreground">
              {stateLabels[session.display_state]}
            </span>
          </div>
          <div className="flex items-center gap-1.5 shrink-0">
            {!hideTimelineButton && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant={isTimelineActive ? "default" : "ghost"}
                    size="sm"
                    className="h-7 w-7 p-0"
                    onClick={(e) => { e.stopPropagation(); onTimelineClick(session.session_id) }}
                  >
                    <List className="h-4 w-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>View session timeline</TooltipContent>
              </Tooltip>
            )}
            {session.peekable && session.display_state !== "dead" ? (
              <span onClick={(e) => e.stopPropagation()}>
                <PeekSwitchButton
                  sessionId={session.session_id}
                  sessionName={name}
                  cmuxAvailable={cmuxAvailable}
                  highlightColor={
                    session.needs_input ? "amber" :
                    session.blocked ? "red" :
                    session.unread_count > 0 ? "blue" :
                    undefined
                  }
                  onSwitch={onSwitch}
                />
              </span>
            ) : cmuxAvailable && session.display_state !== "dead" ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs"
                    onClick={(e) => { e.stopPropagation(); onSwitch(session.session_id) }}
                  >
                    Switch
                    <Terminal className="h-3 w-3 ml-1" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>Switch to this session in cmux</TooltipContent>
              </Tooltip>
            ) : null}
          </div>
        </div>
      </CardHeader>
      {(session.context_percent > 0 || (expandedInfo && hasSessionInfo(session))) && (
        <div className="px-4 mt-0.5 pb-2 space-y-1.5">
          {session.context_percent > 0 && <ContextBar session={session} />}
          {expandedInfo && hasSessionInfo(session) && (
            <div className="text-xs text-muted-foreground">
              <SessionInfoDetails session={session} />
            </div>
          )}
        </div>
      )}
      {session.blocked && (
        <div className={cn("px-4 pl-8", (nonReminderCount > 0 || reminderCount > 0) ? "pb-1" : "pb-2")}>
          <span className="inline-flex items-center gap-1 text-red-400 text-xs">
            <OctagonAlert className="h-3.5 w-3.5 shrink-0" />
            Blocked{session.blocked_reason ? `: ${session.blocked_reason}` : ""}
          </span>
        </div>
      )}
      {(nonReminderCount > 0 || reminderCount > 0) && (
        <div className="px-4 pb-2 pl-8 flex flex-col gap-0.5">
          {nonReminderCount > 0 && (
            <span className="inline-flex items-start gap-1 text-blue-400 text-xs">
              <Mail className="h-3.5 w-3.5 shrink-0 mt-0.5" />
              <span>
                {nonReminderCount} unread
                {nonReminderTypes && (
                  <span className="text-blue-400/70"> ({nonReminderTypes})</span>
                )}
              </span>
            </span>
          )}
          {reminderCount > 0 && (
            <span className="inline-flex items-center gap-1 text-purple-400 text-xs">
              <Bell className="h-3.5 w-3.5 shrink-0" />
              {reminderCount} reminder{reminderCount !== 1 ? "s" : ""}
            </span>
          )}
        </div>
      )}
      <CardContent className="px-4 pb-3 pt-0">
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <div className="flex items-center gap-2 flex-wrap">
          {showBranch && session.branch && (
            <span className="font-mono text-xs">{session.branch}</span>
          )}
          {session.last_prompt && (
            <span>{timeAgo(session.last_prompt)}</span>
          )}
          {session.subscriptions_count > 0 && (
            <Badge
              variant="outline"
              className="text-xs font-normal cursor-pointer hover:bg-accent gap-1"
              onClick={(e) => { e.stopPropagation(); onResourcesOpen(session.session_id) }}
            >
              {session.subscriptions_breakdown
                ? Object.entries(session.subscriptions_breakdown)
                    .sort(([a], [b]) => a.localeCompare(b))
                    .map(([type, count], i, arr) => {
                      const label = type === "pr" ? (count === 1 ? "PR" : "PRs") : type === "jira" ? "Jira" : formatEventType(type)
                      const hasUnreads = session.unread_breakdown && hasUnreadsForResourceType(type, session.unread_breakdown)
                      return (
                        <span key={type} className={hasUnreads ? "text-blue-400" : ""}>
                          {hasUnreads && <span className="inline-block w-1.5 h-1.5 rounded-full bg-blue-400 mr-0.5 align-middle" />}
                          {count} {label}{i < arr.length - 1 ? "," : ""}
                        </span>
                      )
                    })
                : `${session.subscriptions_count} resource${session.subscriptions_count !== 1 ? "s" : ""}`}
            </Badge>
          )}
          {session.inbox_mode !== "manual" && (
            <Badge variant="outline" className="text-xs font-normal">
              inbox: {session.inbox_mode}
            </Badge>
          )}
          </div>
          {showRepoInfo && session.repo && (
            <span className="font-mono text-xs truncate max-w-[350px]" title={`${session.repo.split("/").pop()}${session.branch ? ` (${session.branch})` : ""}`}>
              {session.repo.split("/").pop()}
              {session.branch && ` (${session.branch})`}
            </span>
          )}
        </div>
      </CardContent>
    </Card>
  )

  // Whole-card hover tooltip (model / context / cost) shown to the right, so it
  // doesn't fight the card's own click-to-navigate behavior. Only when the card
  // is clickable — the detail page shows this info inline instead.
  if (onCardClick && hasSessionInfo(session)) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>{card}</TooltipTrigger>
        <TooltipContent side="right">
          <SessionInfoDetails session={session} />
        </TooltipContent>
      </Tooltip>
    )
  }

  return card
}
