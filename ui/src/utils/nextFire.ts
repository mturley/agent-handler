/**
 * Formatting for a cron job's next fire time.
 *
 * The API computes the absolute instant (next_fire_at) from the cron
 * expression; this turns it into the clock time plus a relative hint, e.g.
 * "fires at 1:32pm (in 12 minutes)".
 */

/** "1:32pm", "11:05am" — no leading zero on the hour. */
export function clockTime(d: Date): string {
  const hours = d.getHours()
  const minutes = d.getMinutes()
  const suffix = hours < 12 ? "am" : "pm"
  const h12 = hours % 12 === 0 ? 12 : hours % 12
  return `${h12}:${String(minutes).padStart(2, "0")}${suffix}`
}

/** "in 12 minutes", "in 3 hours", "in 2 days", "in under a minute". */
export function timeUntil(from: Date, to: Date): string {
  const diffMs = to.getTime() - from.getTime()
  if (diffMs <= 0) return "now"

  const minutes = Math.round(diffMs / 60000)
  if (minutes < 1) return "in under a minute"
  if (minutes === 1) return "in 1 minute"
  if (minutes < 60) return `in ${minutes} minutes`

  const hours = Math.round(minutes / 60)
  if (hours === 1) return "in 1 hour"
  if (hours < 24) return `in ${hours} hours`

  const days = Math.round(hours / 24)
  if (days === 1) return "in 1 day"
  return `in ${days} days`
}

/**
 * "fires at 1:32pm (in 12 minutes)", or "" when there is no known next fire
 * (unparseable schedule). A fire more than a day out includes the date, since
 * a bare clock time would be misleading.
 */
export function formatNextFire(nextFireAt: string, now: Date = new Date()): string {
  if (!nextFireAt) return ""
  const next = new Date(nextFireAt)
  if (isNaN(next.getTime())) return ""

  const sameDay = next.toDateString() === now.toDateString()
  const when = sameDay
    ? clockTime(next)
    : `${next.toLocaleDateString(undefined, { month: "short", day: "numeric" })} ${clockTime(next)}`

  return `fires at ${when} (${timeUntil(now, next)})`
}
