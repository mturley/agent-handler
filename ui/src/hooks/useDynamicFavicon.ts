import { useEffect } from "react"
import { useQuery } from "@tanstack/react-query"
import { getSessionsQuickState, getSessions } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"

const baseFavicon = (opts: { needsInput: boolean; hasUnread: boolean }) => {
  let badges = ""
  if (opts.needsInput && opts.hasUnread) {
    badges = '<circle cx="26" cy="6" r="5" fill="#F59E0B"/><circle cx="26" cy="26" r="5" fill="#3B82F6"/>'
  } else if (opts.needsInput) {
    badges = '<circle cx="26" cy="6" r="5" fill="#F59E0B"/>'
  } else if (opts.hasUnread) {
    badges = '<circle cx="26" cy="6" r="5" fill="#3B82F6"/>'
  }
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
  <line x1="16" y1="16" x2="6" y2="6" stroke="#3B82F6" stroke-width="1.5"/>
  <line x1="16" y1="16" x2="26" y2="6" stroke="#3B82F6" stroke-width="1.5"/>
  <line x1="16" y1="16" x2="6" y2="26" stroke="#3B82F6" stroke-width="1.5"/>
  <line x1="16" y1="16" x2="26" y2="26" stroke="#3B82F6" stroke-width="1.5"/>
  <circle cx="6" cy="6" r="3" fill="#3B82F6"/>
  <circle cx="26" cy="6" r="3" fill="#3B82F6"/>
  <circle cx="6" cy="26" r="3" fill="#3B82F6"/>
  <circle cx="26" cy="26" r="3" fill="#3B82F6"/>
  <line x1="9" y1="9" x2="23" y2="23" stroke="#DA7756" stroke-width="3" stroke-linecap="round"/>
  <line x1="23" y1="9" x2="9" y2="23" stroke="#DA7756" stroke-width="3" stroke-linecap="round"/>
  <line x1="16" y1="8" x2="16" y2="24" stroke="#DA7756" stroke-width="3" stroke-linecap="round"/>
  <line x1="8" y1="16" x2="24" y2="16" stroke="#DA7756" stroke-width="3" stroke-linecap="round"/>
  <circle cx="16" cy="16" r="2.5" fill="#DA7756"/>
  ${badges}
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
