import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import type { Session } from "@/api/types"
import { timeAgo } from "@/utils/timeAgo"
import { cn } from "@/lib/utils"

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
        session.unread_count > 0 && "border-l-4 border-l-blue-500"
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
              <span className="text-amber-500 shrink-0" title="Needs input">
                &#9995;
              </span>
            )}
            <span className="text-xs text-muted-foreground">
              {stateLabels[session.display_state]}
            </span>
          </div>
          <div className="flex items-center gap-1.5 shrink-0">
            {session.unread_count > 0 && (
              <Badge
                variant="secondary"
                className="cursor-pointer hover:bg-accent"
                onClick={() => onInboxOpen(session.session_id)}
              >
                {session.unread_count} unread
              </Badge>
            )}
            {cmuxAvailable && session.display_state !== "dead" && (
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-xs"
                onClick={() => onSwitch(session.session_id)}
              >
                Switch
              </Button>
            )}
          </div>
        </div>
      </CardHeader>
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
              {session.subscriptions_count} resource{session.subscriptions_count !== 1 ? "s" : ""}
            </Badge>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
