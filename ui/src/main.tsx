import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { Router } from "wouter"
import { TooltipProvider } from "@/components/ui/tooltip"
import { captureHomeSession } from "@/lib/homeSession"
import { useHomeLocation } from "@/lib/useHomeLocation"
import "./index.css"
import App from "./App"

// Resolve this tab's home session before first render, so the home marker is
// on the URL by the time anything reads or replaces it.
captureHomeSession()

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchOnWindowFocus: false,
    },
  },
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={0}>
        <Router hook={useHomeLocation}>
          <App />
        </Router>
      </TooltipProvider>
    </QueryClientProvider>
  </StrictMode>
)
