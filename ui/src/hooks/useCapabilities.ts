import { useQuery } from "@tanstack/react-query"
import { getCapabilities } from "@/api/client"
import { queryKeys } from "@/api/queryKeys"

export function useCapabilities() {
  const { data } = useQuery({
    queryKey: queryKeys.capabilities,
    queryFn: getCapabilities,
  })
  return data ?? null
}
