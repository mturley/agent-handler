import { useState, useCallback } from "react"
import { useQuery } from "@tanstack/react-query"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog"
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card"
import { Eye, ArrowUpRight } from "lucide-react"
import { cn } from "@/lib/utils"
import { getSessionPeek } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"

interface PeekSwitchButtonProps {
  sessionId: string
  sessionName: string
  cmuxAvailable: boolean
  highlightColor?: "amber" | "red" | "blue"
  onSwitch: (id: string) => void
}

const highlightClass: Record<string, string> = {
  amber: "border-2 border-amber-500/50 bg-amber-950/20",
  red: "border-2 border-red-500/50 bg-red-950/20",
  blue: "border-2 border-blue-500/50 bg-blue-950/20",
}

const highlightTerminalBg: Record<string, string> = {
  amber: "bg-[#1a1408]",
  red: "bg-[#1a0808]",
  blue: "bg-[#08101a]",
}

export function PeekSwitchButton({
  sessionId,
  sessionName,
  cmuxAvailable,
  highlightColor,
  onSwitch,
}: PeekSwitchButtonProps) {
  const [modalOpen, setModalOpen] = useState(false)
  const [hovered, setHovered] = useState(false)

  const isActive = hovered || modalOpen
  const { data: peekState } = useQuery({
    queryKey: queryKeys.peek(sessionId),
    queryFn: () => getSessionPeek(sessionId),
    enabled: isActive,
    staleTime: 5_000,
    refetchInterval: isActive ? 5_000 : false,
  })

  const rawContent = peekState?.content || ""

  // Truncate below the /watching line in the hover preview to strip
  // statusline hints. Keep inbox/inbox-mode/watching but drop everything after.
  // Skip truncation when awaiting approval (statusline layout differs).
  const trimmedContent = (() => {
    if (!rawContent || peekState?.needs_input) return rawContent
    const lines = rawContent.split("\n")
    let watchingIndex = -1
    for (let i = lines.length - 1; i >= 0; i--) {
      if (lines[i].includes("/watching")) {
        watchingIndex = i
        break
      }
    }
    if (watchingIndex === -1) return rawContent
    return lines.slice(0, watchingIndex + 1).join("\n")
  })()

  const scrollToBottom = useCallback((el: HTMLElement | null) => {
    if (el) {
      requestAnimationFrame(() => {
        el.scrollTop = el.scrollHeight
      })
    }
  }, [])

  return (
    <>
      <HoverCard openDelay={300} closeDelay={100}>
        <HoverCardTrigger asChild>
          <div
            className="inline-flex rounded-md border border-input"
            onMouseEnter={() => setHovered(true)}
          >
            <button
              className="inline-flex items-center justify-center h-7 px-2 text-sm rounded-l-md hover:bg-accent hover:text-accent-foreground transition-colors cursor-pointer"
              onClick={() => setModalOpen(true)}
              title="Peek at terminal"
            >
              <Eye className="h-3.5 w-3.5" />
            </button>
            {cmuxAvailable && (
              <button
                className="inline-flex items-center gap-1 h-7 px-2 text-xs border-l border-input rounded-r-md hover:bg-accent hover:text-accent-foreground transition-colors cursor-pointer"
                onClick={() => onSwitch(sessionId)}
                title="Switch to this session in cmux"
              >
                Switch
                <ArrowUpRight className="h-3 w-3" />
              </button>
            )}
          </div>
        </HoverCardTrigger>
        <HoverCardContent
          side="bottom"
          align="end"
          className={cn("w-[90vw] max-w-[900px] p-0", highlightColor && highlightClass[highlightColor])}
        >
          <pre ref={scrollToBottom} className={cn("bg-slate-950 text-slate-300 font-mono text-[11px] leading-tight p-3 rounded-md whitespace-pre-wrap break-all max-h-[50vh] overflow-y-auto overflow-x-hidden", highlightColor && highlightTerminalBg[highlightColor])}>
            {trimmedContent || "No peek data available"}
          </pre>
        </HoverCardContent>
      </HoverCard>

      <Dialog open={modalOpen} onOpenChange={setModalOpen}>
        <DialogContent className={cn("max-w-[90vw] w-full max-h-[80vh] flex flex-col", highlightColor && highlightClass[highlightColor])}>
          <DialogHeader>
            <DialogTitle>{sessionName} — Terminal Preview</DialogTitle>
          </DialogHeader>
          <div ref={scrollToBottom} className="flex-1 overflow-y-auto min-h-0">
            <pre className={cn("bg-slate-950 text-slate-300 font-mono text-xs leading-tight p-4 rounded-md whitespace-pre-wrap break-all overflow-x-hidden", highlightColor && highlightTerminalBg[highlightColor])}>
              {rawContent || "No peek data available"}
            </pre>
          </div>
          <DialogFooter className="gap-2">
            {cmuxAvailable && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  onSwitch(sessionId)
                  setModalOpen(false)
                }}
              >
                Switch
                <ArrowUpRight className="h-3 w-3 ml-1" />
              </Button>
            )}
            <Button variant="secondary" size="sm" onClick={() => setModalOpen(false)}>
              Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
