import { useEffect, useRef } from "react"
import { useQuery } from "@tanstack/react-query"
import { getSessionsQuickState, getSessions } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"

type DotColors = [string, string, string, string] // TL, TR, BL, BR
type DotSizes = [string, string, string, string]

function buildFavicon(colors: DotColors, sizes: DotSizes) {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
  <line x1="16" y1="16" x2="6" y2="6" stroke="#6B7280" stroke-width="1.5"/>
  <line x1="16" y1="16" x2="26" y2="6" stroke="#6B7280" stroke-width="1.5"/>
  <line x1="16" y1="16" x2="6" y2="26" stroke="#6B7280" stroke-width="1.5"/>
  <line x1="16" y1="16" x2="26" y2="26" stroke="#6B7280" stroke-width="1.5"/>
  <circle cx="6" cy="6" r="${sizes[0]}" fill="${colors[0]}"/>
  <circle cx="26" cy="6" r="${sizes[1]}" fill="${colors[1]}"/>
  <circle cx="6" cy="26" r="${sizes[2]}" fill="${colors[2]}"/>
  <circle cx="26" cy="26" r="${sizes[3]}" fill="${colors[3]}"/>
  <line x1="9" y1="9" x2="23" y2="23" stroke="#DA7756" stroke-width="3" stroke-linecap="round"/>
  <line x1="23" y1="9" x2="9" y2="23" stroke="#DA7756" stroke-width="3" stroke-linecap="round"/>
  <line x1="16" y1="8" x2="16" y2="24" stroke="#DA7756" stroke-width="3" stroke-linecap="round"/>
  <line x1="8" y1="16" x2="24" y2="16" stroke="#DA7756" stroke-width="3" stroke-linecap="round"/>
  <circle cx="16" cy="16" r="2.5" fill="#DA7756"/>
</svg>`
}

const LARGE = "5"
const SMALL = "3"

// Clockwise pulse order: TL(0) -> TR(1) -> BR(3) -> BL(2)
const PULSE_ORDER = [0, 1, 3, 2] as const

function getPulseFrames(
  baseColors: DotColors,
): Array<{ colors: DotColors; sizes: DotSizes }> {
  return PULSE_ORDER.map((activeIdx) => {
    const sizes: DotSizes = [SMALL, SMALL, SMALL, SMALL]
    sizes[activeIdx] = LARGE
    return { colors: baseColors, sizes }
  })
}

function setFavicon(svg: string) {
  const dataUrl = `data:image/svg+xml,${encodeURIComponent(svg)}`
  let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!link) {
    link = document.createElement("link")
    link.rel = "icon"
    link.type = "image/svg+xml"
    document.head.appendChild(link)
  }
  link.href = dataUrl
}

export function useDynamicFavicon() {
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const frameRef = useRef(0)

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
    if (intervalRef.current) {
      clearInterval(intervalRef.current)
      intervalRef.current = null
    }

    if (!quickState) return

    const needsInput = Object.values(quickState).some((s) => s.needs_input)
    const hasUnread = sessions?.some((s) => {
      if (s.unread_count === 0) return false
      if (!s.unread_breakdown) return true
      return Object.keys(s.unread_breakdown).some((t) => t !== "reminder")
    }) ?? false

    if (needsInput) {
      // Animated: pulse dots clockwise
      const baseColors: DotColors =
        needsInput && hasUnread
          ? ["#3B82F6", "#F59E0B", "#3B82F6", "#F59E0B"]
          : ["#F59E0B", "#F59E0B", "#F59E0B", "#F59E0B"]

      const frames = getPulseFrames(baseColors)
      frameRef.current = 0
      setFavicon(buildFavicon(frames[0].colors, frames[0].sizes))

      intervalRef.current = setInterval(() => {
        frameRef.current = (frameRef.current + 1) % frames.length
        const f = frames[frameRef.current]
        setFavicon(buildFavicon(f.colors, f.sizes))
      }, 400)
    } else if (hasUnread) {
      // Static: all blue, large
      const colors: DotColors = ["#3B82F6", "#3B82F6", "#3B82F6", "#3B82F6"]
      const sizes: DotSizes = [LARGE, LARGE, LARGE, LARGE]
      setFavicon(buildFavicon(colors, sizes))
    } else {
      // Static: idle
      const colors: DotColors = ["#FFFFFF", "#FFFFFF", "#FFFFFF", "#FFFFFF"]
      const sizes: DotSizes = [SMALL, SMALL, SMALL, SMALL]
      setFavicon(buildFavicon(colors, sizes))
    }

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current)
        intervalRef.current = null
      }
    }
  }, [quickState, sessions])
}
