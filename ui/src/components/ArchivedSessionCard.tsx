import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import type { Session } from "@/api/types"
import { timeAgo } from "@/utils/timeAgo"
import { List } from "lucide-react"
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip"

interface ArchivedSessionCardProps {
  session: Session
  onTimelineClick: (id: string) => void
}

export function ArchivedSessionCard({ session, onTimelineClick }: ArchivedSessionCardProps) {
  const name = session.session_name || session.session_id.slice(0, 12)
  const cwd = session.cwd || session.repo || ""
  const displayDir = cwd.replace(/^\/Users\/[^/]+/, "~")

  return (
    <Card className="border-muted/50">
      <CardHeader className="pb-2 pt-3 px-4">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0">
            <div className="w-2 h-2 rounded-full shrink-0 bg-slate-500" />
            <span className="font-semibold text-sm truncate">{name}</span>
            <span className="text-xs text-muted-foreground">Archived</span>
          </div>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-7 p-0"
                onClick={() => onTimelineClick(session.session_id)}
              >
                <List className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>View session timeline</TooltipContent>
          </Tooltip>
        </div>
      </CardHeader>
      <CardContent className="px-4 pb-3 pt-0 space-y-1">
        <div className="flex items-center gap-2 flex-wrap text-xs text-muted-foreground">
          {session.repo && (
            <Badge variant="outline" className="text-xs font-normal">
              {session.repo.split("/").pop()}
            </Badge>
          )}
          {session.branch && (
            <span className="font-mono">{session.branch}</span>
          )}
          {session.cmux_workspace && (
            <Badge variant="outline" className="text-xs font-normal">
              {session.cmux_workspace}
            </Badge>
          )}
          {session.last_prompt && (
            <span>{timeAgo(session.last_prompt)}</span>
          )}
        </div>
        {displayDir && (
          <div className="text-xs font-mono text-muted-foreground/70">{displayDir}</div>
        )}
        <div className="text-xs font-mono text-muted-foreground/50">{session.session_id}</div>
      </CardContent>
    </Card>
  )
}
