import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import type { Session } from "@/api/types"
import { timeAgo } from "@/utils/timeAgo"
import { cn } from "@/lib/utils"
import { CircleAlert, ArrowUpRight, Mail } from "lucide-react"
import { formatEventType } from "@/utils/formatLabel"

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

interface SessionCardProps {
  session: Session
  showRepoBadge?: boolean
  showBranch?: boolean
  cmuxAvailable: boolean
  onSwitch: (id: string) => void
  onInboxOpen: (id: string) => void
}

export function SessionCard({
  session,
  showRepoBadge,
  showBranch = true,
  cmuxAvailable,
  onSwitch,
  onInboxOpen,
}: SessionCardProps) {
  const name = session.session_name || session.session_id.slice(0, 12)

  return (
    <Card
      className={cn(
        "transition-colors",
        session.needs_input && "border-amber-500/50",
        session.unread_count > 0 && !session.needs_input && "border-blue-500/50"
      )}
    >
      <CardHeader className="pb-2 pt-3 px-4">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0">
            <div
              className={cn("w-2 h-2 rounded-full shrink-0", stateColors[session.display_state])}
            />
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
            {cmuxAvailable && session.display_state !== "dead" && (
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-xs"
                onClick={() => onSwitch(session.session_id)}
              >
                Switch
                <ArrowUpRight className="h-3 w-3 ml-1" />
              </Button>
            )}
          </div>
        </div>
      </CardHeader>
      {session.unread_count > 0 && (
        <div
          className="px-4 pb-0 -mt-1 pl-8 cursor-pointer"
          onClick={() => onInboxOpen(session.session_id)}
        >
          <span className="inline-flex items-center gap-1 text-blue-400 hover:text-blue-300 text-xs">
            <Mail className="h-3.5 w-3.5" />
            {session.unread_breakdown
              ? Object.entries(session.unread_breakdown)
                  .sort(([a], [b]) => a.localeCompare(b))
                  .map(([type, count]) => `${count} ${formatEventType(type)}`)
                  .join(", ")
              : `${session.unread_count} unread`}
          </span>
        </div>
      )}
      <CardContent className="px-4 pb-3 pt-0">
        <div className="flex items-center gap-2 flex-wrap text-xs text-muted-foreground">
          {showRepoBadge && session.repo && (
            <Badge variant="outline" className="text-xs font-normal">
              {session.repo.split("/").pop()}
            </Badge>
          )}
          {showRepoBadge && session.cmux_workspace && (
            <Badge variant="outline" className="text-xs font-normal">
              {session.cmux_workspace}
            </Badge>
          )}
          {showBranch && session.branch && (
            <span className="font-mono text-xs">{session.branch}</span>
          )}
          {session.last_prompt && (
            <span>{timeAgo(session.last_prompt)}</span>
          )}
          {session.subscriptions_count > 0 && (
            <Badge variant="outline" className="text-xs font-normal">
              {session.subscriptions_breakdown
                ? Object.entries(session.subscriptions_breakdown)
                    .sort(([a], [b]) => a.localeCompare(b))
                    .map(([type, count]) => {
                      const label = type === "github_pr" ? "PR" : type === "jira_issue" ? "Jira" : formatEventType(type)
                      return `${count} ${label}`
                    })
                    .join(", ")
                : `${session.subscriptions_count} resource${session.subscriptions_count !== 1 ? "s" : ""}`}
            </Badge>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
