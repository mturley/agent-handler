import { useQuery } from "@tanstack/react-query"
import { cn } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"
import { ScrollArea } from "@/components/ui/scroll-area"
import { getSessionCrons, type SessionCron } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"
import { timeAgo } from "@/utils/timeAgo"
import { formatNextFire } from "@/utils/nextFire"
import { Repeat, Clock } from "lucide-react"

/**
 * Claude Code cron jobs are in-memory and session-scoped: they die with the
 * session, one-shot jobs auto-delete the moment they fire, and recurring jobs
 * auto-expire after 7 days. The list here is reconciled every turn against the
 * Stop hook's session_crons snapshot, so it reflects what is actually live.
 */
export function CronItem({ cron }: { cron: SessionCron }) {
  const nextFire = formatNextFire(cron.next_fire_at)

  return (
    <div className="py-2 px-2 rounded space-y-1">
      <div className="flex items-start gap-2">
        {cron.recurring ? (
          <Repeat className="h-3.5 w-3.5 mt-0.5 shrink-0 text-muted-foreground" />
        ) : (
          <Clock className="h-3.5 w-3.5 mt-0.5 shrink-0 text-muted-foreground" />
        )}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 flex-wrap">
            <code className="text-sm font-medium">{cron.schedule}</code>
            <Badge
              variant="secondary"
              className={cn(
                "px-1.5 py-0 text-[10px] font-normal",
                !cron.recurring && "text-muted-foreground",
              )}
            >
              {cron.recurring ? "recurring" : "one-shot"}
            </Badge>
            <span className="text-[10px] font-mono text-muted-foreground/60">{cron.job_id}</span>
          </div>

          {cron.prompt && (
            <p className="text-xs text-muted-foreground mt-1 whitespace-pre-wrap break-words">
              {cron.prompt}
            </p>
          )}

          <p className="text-[10px] text-muted-foreground/60 mt-1">
            created {timeAgo(cron.created_at)}
            {nextFire && <> · {nextFire}</>}
            {cron.last_seen_at && cron.last_seen_at !== cron.created_at && (
              <> · confirmed {timeAgo(cron.last_seen_at)}</>
            )}
          </p>
        </div>
      </div>
    </div>
  )
}

export function CronsContent({
  sessionId,
  scrollClassName,
}: {
  sessionId: string
  scrollClassName?: string
}) {
  const { data: crons, isLoading } = useQuery({
    queryKey: queryKeys.sessionCrons(sessionId),
    queryFn: () => getSessionCrons(sessionId),
  })

  if (isLoading) {
    return <p className="text-sm text-muted-foreground p-4">Loading...</p>
  }

  if (!crons || crons.length === 0) {
    return (
      <div className="p-4 space-y-1">
        <p className="text-sm text-muted-foreground">No cron jobs scheduled.</p>
        <p className="text-xs text-muted-foreground/60">
          Jobs scheduled with CronCreate appear here. They live only in the session that created
          them.
        </p>
      </div>
    )
  }

  return (
    <ScrollArea className={cn(scrollClassName)}>
      <div className="divide-y">
        {crons.map((c) => (
          <CronItem key={c.job_id} cron={c} />
        ))}
      </div>
    </ScrollArea>
  )
}
