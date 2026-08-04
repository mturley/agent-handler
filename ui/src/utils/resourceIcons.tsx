import { Bug, BookOpen, Layers, CheckSquare, ListTree, GitPullRequest, GitMerge, XCircle } from "lucide-react"
import { cn } from "@/lib/utils"

const jiraFallbackIcons: Record<string, typeof Bug> = {
  Bug: Bug,
  Story: BookOpen,
  Epic: Layers,
  Task: CheckSquare,
  "Sub-task": ListTree,
}

const prStateIcons: Record<string, { icon: typeof GitPullRequest; color: string }> = {
  OPEN: { icon: GitPullRequest, color: "text-green-500" },
  MERGED: { icon: GitMerge, color: "text-purple-500" },
  CLOSED: { icon: XCircle, color: "text-red-500" },
}

export function JiraIssueTypeIcon({ issueType, className }: { issueType?: string; className?: string }) {
  if (!issueType) return null
  const Icon = jiraFallbackIcons[issueType]
  if (!Icon) return null
  return <Icon className={cn("h-3.5 w-3.5 shrink-0", className)} />
}

export function PRStateIcon({ state, className }: { state?: string; className?: string }) {
  if (!state) return null
  const config = prStateIcons[state]
  if (!config) return null
  const Icon = config.icon
  return <Icon className={cn("h-3.5 w-3.5 shrink-0", config.color, className)} />
}
