type EventColorScheme = {
  dot: string
  badge: string
}

const blue: EventColorScheme = { dot: "bg-blue-500", badge: "bg-blue-500/20 text-blue-300 border-blue-500/30" }
const purple: EventColorScheme = { dot: "bg-purple-500", badge: "bg-purple-500/20 text-purple-300 border-purple-500/30" }
const green: EventColorScheme = { dot: "bg-green-500", badge: "bg-green-500/20 text-green-300 border-green-500/30" }
const amber: EventColorScheme = { dot: "bg-amber-500", badge: "bg-amber-500/20 text-amber-300 border-amber-500/30" }
const indigo: EventColorScheme = { dot: "bg-indigo-500", badge: "bg-indigo-500/20 text-indigo-300 border-indigo-500/30" }
const orange: EventColorScheme = { dot: "bg-orange-500", badge: "bg-orange-500/20 text-orange-300 border-orange-500/30" }
const red: EventColorScheme = { dot: "bg-red-500", badge: "bg-red-500/20 text-red-300 border-red-500/30" }
const gray: EventColorScheme = { dot: "bg-gray-400", badge: "bg-gray-500/20 text-gray-300 border-gray-500/30" }

const EVENT_COLORS: Record<string, EventColorScheme> = {
  milestone: blue,
  decision: purple,
  status: gray,
  blocked: amber,
  unblocked: amber,
  message: indigo,
  pr_comment: blue,
  pr_review_comment: blue,
  pr_approved: green,
  pr_merged: purple,
  pr_closed: gray,
  ci_check_passed: green,
  ci_check_failed: red,
  jira_comment: blue,
  jira_status_change: blue,
  jira_assigned: blue,
  jira_labels_changed: blue,
  jira_description_changed: blue,
  handoff: orange,
  followup: orange,
  session_end: gray,
  watch_started: gray,
  watcher_error: red,
}

export function eventDotColor(type: string): string {
  return (EVENT_COLORS[type] || gray).dot
}

export function eventBadgeVariant(type: string): string {
  return (EVENT_COLORS[type] || gray).badge
}
