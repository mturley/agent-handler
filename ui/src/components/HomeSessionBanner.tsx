import { useLocation } from "wouter"
import { useQuery } from "@tanstack/react-query"
import { ArrowLeft } from "lucide-react"
import { getSessions } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"
import { getHomeSession, shouldShowHomeBanner } from "@/lib/homeSession"

/**
 * "Back to <session>" — shown in a tab that was opened FOR a session, on every
 * page except that session's own detail page.
 *
 * Deliberately not dismissible: an aid that can be hidden is missing exactly
 * when it is wanted, and it already hides itself on the page it points at. In
 * a tab nobody homed — most of them — it renders nothing.
 */
export function HomeSessionBanner() {
  const [location, navigate] = useLocation()
  const home = getHomeSession()

  // Reuses the sessions query's cache; no extra request in practice.
  const { data: sessions } = useQuery({
    queryKey: queryKeys.sessions,
    queryFn: getSessions,
    enabled: Boolean(home),
  })

  if (!shouldShowHomeBanner(home, location.split("?")[0])) return null

  const session = sessions?.find((s) => s.session_id === home)
  const label = session?.session_name || session?.branch || home

  return (
    <button
      type="button"
      onClick={() => navigate(`/sessions/${encodeURIComponent(home!)}`)}
      className="mb-4 flex w-full items-center justify-center gap-2 rounded-md border border-input bg-muted/50 px-4 py-2 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground"
    >
      <ArrowLeft className="size-4" />
      Back to <span className="font-semibold">{label}</span>
    </button>
  )
}
