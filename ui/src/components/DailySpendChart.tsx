import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip"

export interface DailySpendEntry {
  date: string
  cost_usd: number
  session_count?: number
}

function formatCost(v: number): string {
  return "$" + v.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

/**
 * A bar-per-day spend chart with hover tooltips. Shared by the cost overview
 * dialog and the single-session Cost tab.
 */
export function DailySpendChart({ days, height = 80 }: { days: DailySpendEntry[]; height?: number }) {
  if (!days || days.length === 0) {
    return <p className="text-sm text-muted-foreground py-8 text-center">No spend recorded.</p>
  }
  const maxDailyCost = Math.max(...days.map((d) => d.cost_usd), 1)

  return (
    <div>
      <div className="flex items-end gap-[2px]" style={{ height }}>
        {days.map((day) => {
          const barHeight = Math.max(Math.round((day.cost_usd / maxDailyCost) * height), 2)
          const dateObj = new Date(day.date + "T00:00:00")
          const dayLabel = dateObj.toLocaleDateString("en-US", { weekday: "short", month: "short", day: "numeric" })
          return (
            <Tooltip key={day.date}>
              <TooltipTrigger asChild>
                <div className="flex-1 flex flex-col items-center justify-end cursor-default">
                  <div
                    className="w-full bg-green-500/60 rounded-t-sm hover:bg-green-400/80 transition-colors"
                    style={{ height: barHeight }}
                  />
                </div>
              </TooltipTrigger>
              <TooltipContent>
                <div className="text-xs">
                  <div className="font-medium">{dayLabel}</div>
                  <div>{formatCost(day.cost_usd)}</div>
                  {day.session_count != null && day.session_count > 1 && (
                    <div className="text-muted-foreground">{day.session_count} sessions</div>
                  )}
                </div>
              </TooltipContent>
            </Tooltip>
          )
        })}
      </div>
      <div className="flex gap-[2px] mt-0.5">
        {days.map((day) => {
          const dateObj = new Date(day.date + "T00:00:00")
          return (
            <div key={day.date} className="flex-1 text-center">
              <span className="text-[9px] text-muted-foreground">{dateObj.getDate()}</span>
            </div>
          )
        })}
      </div>
    </div>
  )
}
