import type { Session } from "@/api/types"
import { cn } from "@/lib/utils"

function formatCost(n: number): string {
  return n.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

/** Whether a session has any model/context/cost info worth surfacing. */
export function hasSessionInfo(session: Session): boolean {
  return session.context_percent > 0 || !!session.model || session.true_cost_usd != null
}

/**
 * Model, context-usage, and cost details for a session. Rendered inside the
 * session card's hover tooltip and inline on the single-session detail page.
 */
export function SessionInfoDetails({ session }: { session: Session }) {
  return (
    <div className="space-y-0.5">
      {session.model && <div className="font-medium">{session.model}</div>}
      {session.context_percent > 0 && <div>{session.context_percent}% context used</div>}
      {session.true_cost_usd != null && (
        <div>
          ${formatCost(session.true_cost_usd)} total
          {session.today_cost_usd != null && session.today_cost_usd > 0 && (
            <span> (${formatCost(session.today_cost_usd)} today)</span>
          )}
        </div>
      )}
    </div>
  )
}

/** The thin context-usage bar (no tooltip wrapper). */
export function ContextBar({ session, className }: { session: Session; className?: string }) {
  if (session.context_percent <= 0) return null
  return (
    <div className={cn("w-full h-1.5 bg-muted rounded-full overflow-hidden", className)}>
      <div
        className={cn(
          "h-full rounded-full transition-all",
          session.context_percent >= 80 ? "bg-red-400/60" :
          session.context_percent >= 50 ? "bg-yellow-400/50" : "bg-green-400/40"
        )}
        style={{ width: `${session.context_percent}%` }}
      />
    </div>
  )
}
