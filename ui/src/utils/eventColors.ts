export function eventDotColor(type: string): string {
  switch (type) {
    case "milestone": return "bg-blue-500"
    case "decision": return "bg-purple-500"
    case "status": return "bg-gray-400"
    case "blocked": case "unblocked": return "bg-amber-500"
    case "message": return "bg-indigo-500"
    case "pr_comment": case "pr_review_comment": case "pr_approved": return "bg-green-500"
    case "pr_merged": case "pr_closed": return "bg-gray-400"
    case "ci_check_passed": return "bg-green-500"
    case "ci_check_failed": return "bg-red-500"
    case "jira_comment": case "jira_status_change": case "jira_assigned": return "bg-blue-500"
    case "handoff": case "followup": return "bg-orange-500"
    case "session_end": return "bg-gray-400"
    default: return "bg-gray-400"
  }
}

export function eventBadgeVariant(type: string): string {
  switch (type) {
    case "milestone": return "bg-blue-500/20 text-blue-300 border-blue-500/30"
    case "decision": return "bg-purple-500/20 text-purple-300 border-purple-500/30"
    case "blocked": case "unblocked": return "bg-amber-500/20 text-amber-300 border-amber-500/30"
    case "message": return "bg-indigo-500/20 text-indigo-300 border-indigo-500/30"
    case "ci_check_failed": return "bg-red-500/20 text-red-300 border-red-500/30"
    case "ci_check_passed": case "pr_approved": return "bg-green-500/20 text-green-300 border-green-500/30"
    case "handoff": case "followup": return "bg-orange-500/20 text-orange-300 border-orange-500/30"
    default: return "bg-gray-500/20 text-gray-300 border-gray-500/30"
  }
}
