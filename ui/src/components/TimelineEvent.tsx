import { useState } from "react"
import { Card, CardContent } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import type { TimelineEvent as TimelineEventType } from "@/api/types"
import { formatEventType } from "@/utils/formatLabel"
import { timeAgo } from "@/utils/timeAgo"
import { eventDotColor, eventBadgeVariant } from "@/utils/eventColors"
import { ChevronRight, ChevronDown, ExternalLink } from "lucide-react"
import { cn } from "@/lib/utils"

interface TimelineEventProps {
  event: TimelineEventType
  onSessionClick?: (sessionName: string) => void
}

export function TimelineEvent({ event, onSessionClick }: TimelineEventProps) {
  const [expanded, setExpanded] = useState(false)
  const hasBody = event.body && event.body.trim().length > 0
  const hasResources = event.resources && event.resources.length > 0

  const dotColor = eventDotColor(event.type)
  const badgeClasses = eventBadgeVariant(event.type)

  return (
    <div className="relative flex gap-4">
      {/* Timeline dot and connector */}
      <div className="absolute -left-[26px] top-1/2 -translate-y-1/2 flex items-center">
        <div className={cn("w-3.5 h-3.5 rounded-full shrink-0", dotColor)} />
        <div className="w-[12px] h-0 border-t border-slate-700" />
      </div>

      {/* Event bubble */}
      <Card className="flex-1">
        <CardContent className="p-4 space-y-2">
          {/* Header: type badge, title, timestamp */}
          <div className="flex items-start gap-2 flex-wrap">
            <Badge variant="outline" className={cn("text-xs shrink-0", badgeClasses)}>
              {formatEventType(event.type)}
            </Badge>
            <div className="flex-1 min-w-0">
              <p className="font-semibold text-sm leading-tight break-words">
                {event.title}
              </p>
            </div>
            <span className="text-xs text-muted-foreground shrink-0">
              {timeAgo(event.ts)}
            </span>
          </div>

          {/* Meta: session name, author */}
          <div className="flex items-center gap-2 text-xs text-muted-foreground flex-wrap">
            {event.session_name && (
              <span
                className={cn(
                  "font-mono",
                  onSessionClick && "cursor-pointer hover:text-foreground"
                )}
                onClick={() => onSessionClick?.(event.session_name!)}
              >
                {event.session_name}
              </span>
            )}
            {event.author && (
              <span>
                by {event.author}
              </span>
            )}
          </div>

          {/* Resource links */}
          {hasResources && (
            <div className="flex gap-2 flex-wrap">
              {event.resources.map((resource, i) => (
                <a
                  key={i}
                  href={resource.resource_url || "#"}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-xs text-blue-400 hover:text-blue-300 hover:underline"
                >
                  <span>
                    {resource.resource_type === "pr" && "PR"}
                    {resource.resource_type === "jira" && resource.resource_id}
                    {resource.resource_type !== "pr" && resource.resource_type !== "jira" && resource.resource_id}
                  </span>
                  {resource.resource_type === "pr" && (
                    <span>#{resource.resource_id}</span>
                  )}
                  <ExternalLink className="h-3 w-3" />
                </a>
              ))}
            </div>
          )}

          {/* Body (expandable) */}
          {hasBody && (
            <>
              <Button
                variant="ghost"
                size="sm"
                className="h-auto py-1 px-2 text-xs text-muted-foreground hover:text-foreground"
                onClick={() => setExpanded(!expanded)}
              >
                {expanded ? (
                  <>
                    <ChevronDown className="h-3 w-3 mr-1" />
                    Hide details
                  </>
                ) : (
                  <>
                    <ChevronRight className="h-3 w-3 mr-1" />
                    Show more
                  </>
                )}
              </Button>
              {expanded && (
                <div className="text-sm text-foreground whitespace-pre-wrap break-words border-l-2 border-muted pl-3 mt-2">
                  {event.body}
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
