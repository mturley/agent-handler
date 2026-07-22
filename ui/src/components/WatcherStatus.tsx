import { CheckCircle, XCircle } from "lucide-react"
import { Card, CardContent } from "@/components/ui/card"
import type { WatcherStatusInfo } from "@/api/types"
import { timeAgo } from "@/utils/timeAgo"

interface WatcherStatusProps {
  watcherStatus: Record<string, WatcherStatusInfo>
}

export function WatcherStatus({ watcherStatus }: WatcherStatusProps) {
  const services = Object.entries(watcherStatus)

  if (services.length === 0) {
    return null
  }

  return (
    <Card className="mb-4">
      <CardContent className="px-4 py-3">
        <div className="flex items-center gap-6 flex-wrap">
          {services.map(([service, status]) => {
            const healthy = status.configured && status.installed && !status.has_error
            const displayName = service === "github" ? "GitHub" : service === "jira" ? "Jira" : service

            if (!status.configured) {
              return (
                <div key={service} className="flex items-center gap-2">
                  <span className="font-medium text-sm">{displayName}</span>
                  <span className="text-xs text-muted-foreground">not configured</span>
                </div>
              )
            }

            return (
              <div key={service} className="flex items-center gap-2">
                {healthy ? (
                  <CheckCircle className="h-4 w-4 text-green-500" />
                ) : (
                  <XCircle className="h-4 w-4 text-red-500" />
                )}
                <span className="font-medium text-sm">{displayName}</span>
                {status.last_success && (
                  <span className="text-xs text-muted-foreground">
                    last run {timeAgo(status.last_success)}
                  </span>
                )}
                {status.has_error && status.last_error && (
                  <span className="text-xs text-red-400">
                    {status.last_error}
                  </span>
                )}
              </div>
            )
          })}
        </div>
      </CardContent>
    </Card>
  )
}
