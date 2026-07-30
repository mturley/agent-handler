import { useQuery } from "@tanstack/react-query"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { getCostSummary } from "@/api/client"

interface CostDialogProps {
  open: boolean
  onClose: () => void
}

function formatCost(v: number): string {
  return "$" + v.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return String(n)
}

export function CostDialog({ open, onClose }: CostDialogProps) {
  const { data } = useQuery({
    queryKey: ["cost"],
    queryFn: getCostSummary,
    enabled: open,
  })

  if (!data || !data.enabled) return null

  const maxDailyCost = Math.max(...(data.daily_breakdown || []).map((d) => d.cost_usd), 1)

  return (
    <Dialog open={open} onOpenChange={() => onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Cost Summary</DialogTitle>
        </DialogHeader>

        <ScrollArea className="max-h-[500px]">
          <div className="space-y-6">
            {/* Summary stats */}
            <div className="grid grid-cols-3 gap-4">
              <div className="text-center">
                <div className="text-2xl font-bold">{formatCost(data.today_cost_usd)}</div>
                <div className="text-xs text-muted-foreground">Today</div>
              </div>
              <div className="text-center">
                <div className="text-2xl font-bold">{formatCost(data.month_cost_usd)}</div>
                <div className="text-xs text-muted-foreground">This month</div>
              </div>
              <div className="text-center">
                <div className="text-2xl font-bold">{formatCost(data.all_time_cost_usd)}</div>
                <div className="text-xs text-muted-foreground">All time</div>
              </div>
            </div>

            {/* Daily bar chart */}
            {data.daily_breakdown && data.daily_breakdown.length > 0 && (
              <div>
                <h3 className="text-sm font-semibold mb-2">Daily Spend</h3>
                <div className="flex items-end gap-[2px] h-[80px]">
                  {data.daily_breakdown.map((day) => {
                    const height = Math.max((day.cost_usd / maxDailyCost) * 100, 2)
                    const dateObj = new Date(day.date + "T00:00:00")
                    const dayNum = dateObj.getDate()
                    return (
                      <div key={day.date} className="flex-1 flex flex-col items-center gap-0.5" title={`${day.date}: ${formatCost(day.cost_usd)} (${day.session_count} sessions)`}>
                        <div
                          className="w-full bg-green-500/60 rounded-t-sm hover:bg-green-400/80 transition-colors cursor-default"
                          style={{ height: `${height}%` }}
                        />
                        {data.daily_breakdown.length <= 31 && (
                          <span className="text-[9px] text-muted-foreground">{dayNum}</span>
                        )}
                      </div>
                    )
                  })}
                </div>
              </div>
            )}

            {/* Top sessions */}
            {data.top_sessions && data.top_sessions.length > 0 && (
              <div>
                <h3 className="text-sm font-semibold mb-2">Top Sessions This Month</h3>
                <div className="space-y-1">
                  {data.top_sessions.map((session) => (
                    <div key={session.session_id} className="flex items-center justify-between text-sm py-1 px-2 rounded hover:bg-muted/50">
                      <div className="flex items-center gap-2 min-w-0">
                        <span className="font-mono text-xs truncate">
                          {session.session_name || session.session_id.slice(0, 12)}
                        </span>
                      </div>
                      <div className="flex items-center gap-3 shrink-0 text-xs text-muted-foreground">
                        <span>{formatTokens(session.input_tokens)} in / {formatTokens(session.output_tokens)} out</span>
                        <span className="font-medium text-foreground">{formatCost(session.cost_usd)}</span>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}

export function CostBadge({ onClick }: { onClick: () => void }) {
  const { data } = useQuery({
    queryKey: ["cost"],
    queryFn: getCostSummary,
    refetchInterval: 60_000,
  })

  if (!data || !data.enabled) return null

  return (
    <button
      onClick={onClick}
      className="text-xs text-muted-foreground hover:text-foreground transition-colors cursor-pointer select-none"
    >
      <span className="font-medium">{formatCost(data.today_cost_usd)}</span>
      <span className="mx-1">·</span>
      <span>{formatCost(data.month_cost_usd)} this month</span>
    </button>
  )
}
