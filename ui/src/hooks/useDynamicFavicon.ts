import { useEffect } from "react"
import { useQuery } from "@tanstack/react-query"
import { getSessionsQuickState, getSessions } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"

const baseFavicon = (opts: { needsInput: boolean; hasUnread: boolean }) => {
  let dotR = "3"
  let tl = "#FFFFFF", tr = "#FFFFFF", bl = "#FFFFFF", br = "#FFFFFF"

  if (opts.needsInput && opts.hasUnread) {
    dotR = "5"
    tl = "#3B82F6"; tr = "#F59E0B"; bl = "#3B82F6"; br = "#F59E0B"
  } else if (opts.needsInput) {
    dotR = "5"
    tl = "#F59E0B"; tr = "#F59E0B"; bl = "#F59E0B"; br = "#F59E0B"
  } else if (opts.hasUnread) {
    dotR = "5"
    tl = "#3B82F6"; tr = "#3B82F6"; bl = "#3B82F6"; br = "#3B82F6"
  }

  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
  <line x1="16" y1="16" x2="6" y2="6" stroke="#6B7280" stroke-width="1.5"/>
  <line x1="16" y1="16" x2="26" y2="6" stroke="#6B7280" stroke-width="1.5"/>
  <line x1="16" y1="16" x2="6" y2="26" stroke="#6B7280" stroke-width="1.5"/>
  <line x1="16" y1="16" x2="26" y2="26" stroke="#6B7280" stroke-width="1.5"/>
  <circle cx="6" cy="6" r="${dotR}" fill="${tl}"/>
  <circle cx="26" cy="6" r="${dotR}" fill="${tr}"/>
  <circle cx="6" cy="26" r="${dotR}" fill="${bl}"/>
  <circle cx="26" cy="26" r="${dotR}" fill="${br}"/>
  <line x1="9" y1="9" x2="23" y2="23" stroke="#DA7756" stroke-width="3" stroke-linecap="round"/>
  <line x1="23" y1="9" x2="9" y2="23" stroke="#DA7756" stroke-width="3" stroke-linecap="round"/>
  <line x1="16" y1="8" x2="16" y2="24" stroke="#DA7756" stroke-width="3" stroke-linecap="round"/>
  <line x1="8" y1="16" x2="24" y2="16" stroke="#DA7756" stroke-width="3" stroke-linecap="round"/>
  <circle cx="16" cy="16" r="2.5" fill="#DA7756"/>
</svg>`
}

export function useDynamicFavicon() {
  const { data: quickState } = useQuery({
    queryKey: ["sessions", "quickState"],
    queryFn: getSessionsQuickState,
    refetchInterval: 5_000,
    staleTime: 0,
  })

  const { data: sessions } = useQuery({
    queryKey: queryKeys.sessions,
    queryFn: getSessions,
  })

  useEffect(() => {
    if (!quickState) return

    const hasNeedsInput = Object.values(quickState).some((s) => s.needs_input)
    const hasUnread = sessions?.some((s) => s.unread_count > 0) ?? false

    const svg = baseFavicon({ needsInput: hasNeedsInput, hasUnread })
    const dataUrl = `data:image/svg+xml,${encodeURIComponent(svg)}`

    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    if (!link) {
      link = document.createElement("link")
      link.rel = "icon"
      link.type = "image/svg+xml"
      document.head.appendChild(link)
    }
    link.href = dataUrl
  }, [quickState])
}
