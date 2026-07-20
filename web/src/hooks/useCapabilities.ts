import { useState, useEffect } from "react"
import type { Capabilities } from "@/api/types"
import { getCapabilities } from "@/api/client"

export function useCapabilities() {
  const [capabilities, setCapabilities] = useState<Capabilities | null>(null)

  useEffect(() => {
    getCapabilities().then(setCapabilities).catch(console.error)
  }, [])

  return capabilities
}
