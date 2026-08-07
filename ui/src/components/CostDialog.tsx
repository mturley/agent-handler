import { useQuery } from "@tanstack/react-query"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ScrollArea } from "@/components/ui/scroll-area"
import { getCostSummary, type CostMonthSummary } from "@/api/client"
import { DailySpendChart } from "@/components/DailySpendChart"

interface CostDialogProps {
  open: boolean
  onClose: () => void
}

function formatCost(v: number): string {
  return "$" + v.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function MonthView({ month }: { month: CostMonthSummary }) {
  return (
    <div className="space-y-6">
      {/* Daily bar chart */}
      {month.daily_breakdown && month.daily_breakdown.length > 0 && (
        <div>
          <h3 className="text-sm font-semibold mb-2">Daily Spend</h3>
          <DailySpendChart days={month.daily_breakdown} />
        </div>
      )}

      {/* Top sessions */}
      {month.top_sessions && month.top_sessions.length > 0 && (
        <div>
          <h3 className="text-sm font-semibold mb-2">Top Sessions</h3>
          <div className="space-y-1">
            {month.top_sessions.map((session) => (
              <div key={session.session_id} className="flex items-center justify-between text-sm py-1 px-2 rounded hover:bg-muted/50">
                <div className="flex items-center gap-2 min-w-0">
                  <span className="font-mono text-xs truncate">
                    {session.session_name || session.session_id.slice(0, 12)}
                  </span>
                </div>
                <span className="shrink-0 text-xs font-medium">{formatCost(session.cost_usd)}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {month.daily_breakdown?.length === 0 && month.top_sessions?.length === 0 && (
        <p className="text-sm text-muted-foreground text-center py-4">No cost data for this month.</p>
      )}
    </div>
  )
}

export function CostDialog({ open, onClose }: CostDialogProps) {
  const { data } = useQuery({
    queryKey: ["cost"],
    queryFn: getCostSummary,
    enabled: open,
  })

  if (!data || !data.enabled) return null

  const months = [...(data.months || [])].reverse()

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
                <div className="text-2xl font-bold">{months.length > 0 ? formatCost(months[months.length - 1].cost_usd) : "$0.00"}</div>
                <div className="text-xs text-muted-foreground">This month</div>
              </div>
              <div className="text-center">
                <div className="text-2xl font-bold">{formatCost(data.all_time_cost_usd)}</div>
                <div className="text-xs text-muted-foreground">All time</div>
              </div>
            </div>

            {/* Month tabs */}
            {months.length > 0 && (
              <Tabs defaultValue={String(months.length - 1)}>
                <TabsList>
                  {months.map((month, i) => (
                    <TabsTrigger key={i} value={String(i)}>
                      {month.label}
                    </TabsTrigger>
                  ))}
                </TabsList>
                {months.map((month, i) => (
                  <TabsContent key={i} value={String(i)} className="mt-4">
                    <div className="text-lg font-bold mb-4">{month.full_label}: {formatCost(month.cost_usd)}</div>
                    <MonthView month={month} />
                  </TabsContent>
                ))}
              </Tabs>
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

  const months = data.months || []
  const monthCost = months.length > 0 ? months[0].cost_usd : 0

  return (
    <button
      onClick={onClick}
      className="text-xs text-muted-foreground hover:text-foreground transition-colors cursor-pointer select-none"
    >
      <span className="font-medium">{formatCost(data.today_cost_usd)} today</span>
      <span className="mx-1">·</span>
      <span>{formatCost(monthCost)} this month</span>
    </button>
  )
}
