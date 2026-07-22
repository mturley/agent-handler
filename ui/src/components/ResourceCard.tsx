import { useState } from "react"
import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip"
import type { ResourceEntry } from "@/api/types"
import { timeAgo } from "@/utils/timeAgo"
import { cn } from "@/lib/utils"
import {
  ExternalLink,
  ChevronRight,
  ChevronDown,
  ArrowUpRight,
  AlertTriangle,
  Clock,
} from "lucide-react"

interface ResourceCardProps {
  resource: ResourceEntry
  cmuxAvailable: boolean
  onSwitch: (sessionId: string) => void
  onTimelineClick: (resourceType: string, resourceId: string) => void
  onSessionClick: (sessionName: string) => void
}

// PR state interface
interface PRState extends Record<string, unknown> {
  title?: string
  author?: string
  state?: string
  review_decision?: string
  ci_status?: string
  has_new_commits_since_review?: boolean
  is_draft?: boolean
}

// Jira state interface
interface JiraState extends Record<string, unknown> {
  summary?: string
  assignee?: string
  status?: string
  priority?: string
  blocked?: boolean
  blocked_reason?: string
  epic_key?: string
  story_points?: number
  labels?: string[]
}

function isPRState(state: Record<string, unknown> | undefined): state is PRState {
  return state !== undefined && "title" in state
}

function isJiraState(state: Record<string, unknown> | undefined): state is JiraState {
  return state !== undefined && "summary" in state
}

export function ResourceCard({
  resource,
  cmuxAvailable,
  onSwitch,
  onTimelineClick,
  onSessionClick,
}: ResourceCardProps) {
  const [expanded, setExpanded] = useState(false)
  const isPR = resource.resource_type === "pr"
  const prState = isPR && isPRState(resource.state) ? resource.state : undefined
  const jiraState = !isPR && isJiraState(resource.state) ? resource.state : undefined

  const title = prState?.title || jiraState?.summary || resource.resource_id

  return (
    <Card>
      <CardHeader className="pb-2 pt-3 px-4">
        <div className="flex items-start justify-between gap-2">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2">
              <a
                href={resource.resource_url}
                target="_blank"
                rel="noopener noreferrer"
                className="font-semibold text-sm hover:underline truncate"
              >
                {title}
              </a>
              <ExternalLink className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
            </div>

            {/* Badges row */}
            <div className="flex items-center gap-1.5 mt-2 flex-wrap">
              {isPR && prState && (
                <>
                  {/* State badge */}
                  {prState.state && (
                    <Badge
                      variant="outline"
                      className={cn(
                        prState.state === "OPEN" && "border-green-500 text-green-500",
                        prState.state === "MERGED" && "border-purple-500 text-purple-500",
                        prState.state === "CLOSED" && "border-red-500 text-red-500"
                      )}
                    >
                      {prState.state.toLowerCase()}
                    </Badge>
                  )}
                  {/* Review decision */}
                  {prState.review_decision && (
                    <Badge
                      variant="outline"
                      className={cn(
                        prState.review_decision === "APPROVED" && "border-green-500 text-green-500",
                        prState.review_decision === "CHANGES_REQUESTED" &&
                          "border-amber-500 text-amber-500",
                        prState.review_decision === "REVIEW_REQUIRED" &&
                          "border-gray-500 text-gray-500"
                      )}
                    >
                      {prState.review_decision.toLowerCase().replace(/_/g, " ")}
                    </Badge>
                  )}
                  {/* CI status */}
                  {prState.ci_status && (
                    <Badge
                      variant="outline"
                      className={cn(
                        prState.ci_status === "SUCCESS" && "border-green-500 text-green-500",
                        prState.ci_status === "FAILURE" && "border-red-500 text-red-500",
                        prState.ci_status === "PENDING" && "border-yellow-500 text-yellow-500"
                      )}
                    >
                      ci: {prState.ci_status.toLowerCase()}
                    </Badge>
                  )}
                </>
              )}

              {!isPR && jiraState && (
                <>
                  {/* Status badge */}
                  {jiraState.status && (
                    <Badge variant="outline" className="border-blue-500 text-blue-500">
                      {jiraState.status}
                    </Badge>
                  )}
                  {/* Priority badge */}
                  {jiraState.priority && (
                    <Badge
                      variant="outline"
                      className={cn(
                        (jiraState.priority === "Blocker" || jiraState.priority === "Critical") &&
                          "border-red-500 text-red-500",
                        jiraState.priority === "Major" && "border-amber-500 text-amber-500",
                        jiraState.priority === "Normal" && "border-blue-500 text-blue-500",
                        jiraState.priority === "Minor" && "border-gray-500 text-gray-500"
                      )}
                    >
                      {(jiraState.priority === "Blocker" ||
                        jiraState.priority === "Critical") && (
                        <AlertTriangle className="h-3 w-3 mr-1" />
                      )}
                      {jiraState.priority}
                    </Badge>
                  )}
                </>
              )}
            </div>

            {/* Author/Assignee line */}
            {(prState?.author || jiraState?.assignee) && (
              <div className="text-xs text-muted-foreground mt-1">
                {prState?.author && `by ${prState.author}`}
                {jiraState?.assignee && `assigned to ${jiraState.assignee}`}
              </div>
            )}
          </div>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-7 p-0 shrink-0"
                onClick={() => onTimelineClick(resource.resource_type, resource.resource_id)}
              >
                <Clock className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>View resource timeline</TooltipContent>
          </Tooltip>
        </div>
      </CardHeader>

      <CardContent className="px-4 pb-3 pt-0">
        {/* Session pills */}
        {resource.sessions.length > 0 && (
          <div className="flex items-center gap-1.5 flex-wrap mb-2">
            {resource.sessions.map((session) => (
              <div key={session.session_id} className="flex items-center gap-1">
                <Badge
                  variant="outline"
                  className="text-xs font-normal cursor-pointer hover:bg-muted"
                  onClick={() => onSessionClick(session.session_name)}
                >
                  {session.session_name}
                </Badge>
                {cmuxAvailable && session.display_state !== "dead" && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-5 w-5 p-0"
                        onClick={() => onSwitch(session.session_id)}
                      >
                        <ArrowUpRight className="h-3 w-3" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>Switch to session</TooltipContent>
                  </Tooltip>
                )}
              </div>
            ))}
          </div>
        )}

        {/* Collapsible details */}
        <div>
          <button
            onClick={() => setExpanded(!expanded)}
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            {expanded ? (
              <ChevronDown className="h-3 w-3" />
            ) : (
              <ChevronRight className="h-3 w-3" />
            )}
            Details
          </button>

          {expanded && (
            <div className="mt-2 space-y-1 text-xs text-muted-foreground pl-4">
              {isPR && prState && (
                <>
                  {prState.has_new_commits_since_review !== undefined && (
                    <div>
                      New commits since review:{" "}
                      {prState.has_new_commits_since_review ? "yes" : "no"}
                    </div>
                  )}
                  {prState.is_draft !== undefined && (
                    <div>Draft: {prState.is_draft ? "yes" : "no"}</div>
                  )}
                </>
              )}

              {!isPR && jiraState && (
                <>
                  {jiraState.blocked !== undefined && (
                    <div>Blocked: {jiraState.blocked ? "yes" : "no"}</div>
                  )}
                  {jiraState.blocked_reason && (
                    <div className="text-red-400">Blocked reason: {jiraState.blocked_reason}</div>
                  )}
                  {jiraState.epic_key && <div>Epic: {jiraState.epic_key}</div>}
                  {jiraState.story_points !== undefined && (
                    <div>Story points: {jiraState.story_points}</div>
                  )}
                  {jiraState.labels && jiraState.labels.length > 0 && (
                    <div>Labels: {jiraState.labels.join(", ")}</div>
                  )}
                </>
              )}

              {resource.watcher_updated_at && (
                <div>Watcher last checked: {timeAgo(resource.watcher_updated_at)}</div>
              )}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
