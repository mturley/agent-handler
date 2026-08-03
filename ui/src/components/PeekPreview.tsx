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
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip"
import { Eye, ArrowUpRight } from "lucide-react"
import { getSessionPeek } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"

interface PeekPreviewProps {
  sessionId: string
  sessionName: string
  cmuxAvailable: boolean
  onSwitch: (id: string) => void
}

export function PeekPreview({
  sessionId,
  sessionName,
  cmuxAvailable,
  onSwitch,
}: PeekPreviewProps) {
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

  const content = peekState?.content || ""

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
          <span onMouseEnter={() => setHovered(true)}>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 w-7 p-0"
                  onClick={() => setModalOpen(true)}
                >
                  <Eye className="h-4 w-4" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Peek at terminal</TooltipContent>
            </Tooltip>
          </span>
        </HoverCardTrigger>
        <HoverCardContent
          side="bottom"
          align="end"
          className="w-[90vw] max-w-[900px] p-0"
        >
          <pre ref={scrollToBottom} className="bg-slate-950 text-slate-300 font-mono text-[11px] leading-tight p-3 rounded-md whitespace-pre-wrap break-all max-h-[45vh] overflow-y-auto overflow-x-hidden">
            {content || "No peek data available"}
          </pre>
        </HoverCardContent>
      </HoverCard>

      <Dialog open={modalOpen} onOpenChange={setModalOpen}>
        <DialogContent className="max-w-[90vw] w-full max-h-[80vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>{sessionName} — Terminal Preview</DialogTitle>
          </DialogHeader>
          <div ref={scrollToBottom} className="flex-1 overflow-y-auto min-h-0">
            <pre className="bg-slate-950 text-slate-300 font-mono text-xs leading-tight p-4 rounded-md whitespace-pre-wrap break-all overflow-x-hidden">
              {content || "No peek data available"}
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
