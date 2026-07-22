export const queryKeys = {
  sessions: ["sessions"] as const,
  capabilities: ["capabilities"] as const,
  events: (filters?: Record<string, string | undefined>) =>
    ["events", filters] as const,
  inbox: (sessionId: string) => ["inbox", sessionId] as const,
  resources: ["resources"] as const,
}
