import { Bug, BookOpen, Layers, CheckSquare, ListTree, GitPullRequest, GitMerge, XCircle, MessagesSquare } from "lucide-react"
import { cn } from "@/lib/utils"

const jiraIssueTypeConfig: Record<string, { icon: typeof Bug; color: string }> = {
  Bug: { icon: Bug, color: "text-red-500" },
  Story: { icon: BookOpen, color: "text-green-500" },
  Epic: { icon: Layers, color: "text-purple-500" },
  Task: { icon: CheckSquare, color: "text-blue-500" },
  "Sub-task": { icon: ListTree, color: "text-blue-400" },
  Feature: { icon: BookOpen, color: "text-green-500" },
  RFE: { icon: BookOpen, color: "text-green-500" },
}

const prStateIcons: Record<string, { icon: typeof GitPullRequest; color: string }> = {
  OPEN: { icon: GitPullRequest, color: "text-green-500" },
  MERGED: { icon: GitMerge, color: "text-purple-500" },
  CLOSED: { icon: XCircle, color: "text-red-500" },
}

export function JiraIssueTypeIcon({ issueType, className }: { issueType?: string; className?: string }) {
  if (!issueType) return null
  const config = jiraIssueTypeConfig[issueType]
  if (!config) return null
  const Icon = config.icon
  return <Icon className={cn("h-3.5 w-3.5 shrink-0", config.color, className)} />
}

export function PRStateIcon({ state, className }: { state?: string; className?: string }) {
  if (!state) return null
  const config = prStateIcons[state]
  if (!config) return null
  const Icon = config.icon
  return <Icon className={cn("h-3.5 w-3.5 shrink-0", config.color, className)} />
}

export function SlackThreadIcon({ className }: { className?: string }) {
  return <MessagesSquare className={cn("h-3.5 w-3.5 shrink-0 text-fuchsia-400", className)} />
}
