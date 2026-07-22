import { useState } from "react"
import { Card, CardContent } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import type { TimelineEvent as TimelineEventType } from "@/api/types"
import { formatEventType } from "@/utils/formatLabel"
import { timeAgo } from "@/utils/timeAgo"
import { eventDotColor, eventBadgeVariant } from "@/utils/eventColors"
import { ChevronRight, ChevronDown, ExternalLink, OctagonAlert, ArrowUp, ArrowDown, Minus, Equal } from "lucide-react"
import { cn } from "@/lib/utils"

const priorityConfig: Record<string, { icon: typeof ArrowUp; color: string }> = {
  Blocker: { icon: OctagonAlert, color: "text-red-500" },
  Critical: { icon: ArrowUp, color: "text-red-400" },
  Major: { icon: ArrowUp, color: "text-orange-400" },
  Normal: { icon: Equal, color: "text-blue-400" },
  Minor: { icon: ArrowDown, color: "text-green-400" },
  Trivial: { icon: ArrowDown, color: "text-gray-400" },
}

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

          {/* Resource links with metadata */}
          {hasResources && (
            <div className="space-y-1">
              {event.resources.map((resource, i) => {
                const meta = resource.metadata
                const label = resource.resource_type === "pr"
                  ? `PR #${resource.resource_id}`
                  : resource.resource_id
                return (
                  <div key={i} className="text-xs">
                    <a
                      href={resource.resource_url || "#"}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1 text-blue-400 hover:text-blue-300 hover:underline"
                    >
                      {label}
                      <ExternalLink className="h-3 w-3" />
                    </a>
                    {meta?.title && (
                      <span className="text-muted-foreground ml-1.5">{meta.title}</span>
                    )}
                    {resource.resource_type === "pr" && meta?.author && (
                      <span className="text-muted-foreground/60 ml-1.5">by {meta.author}</span>
                    )}
                    {resource.resource_type === "jira" && meta && (
                      <span className="inline-flex items-center gap-1 text-muted-foreground/60 ml-1.5">
                        {meta.priority && (() => {
                          const config = priorityConfig[meta.priority]
                          const Icon = config?.icon || Minus
                          const color = config?.color || "text-muted-foreground"
                          return (
                            <span className={cn("inline-flex items-center gap-0.5", color)}>
                              <Icon className="h-3 w-3" />
                              {meta.priority}
                            </span>
                          )
                        })()}
                        {meta.priority && meta.status && " · "}
                        {meta.status}
                        {meta.status && meta.assignee && " · "}
                        {meta.assignee}
                      </span>
                    )}
                  </div>
                )
              })}
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
