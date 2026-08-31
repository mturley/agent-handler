import type { Session, PeekState, Event, Capabilities, ActionResponse, EventsResponse, ResourcesResponse } from "./types"

const BASE = ""

async function fetchJSON<T>(url: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${url}`, options)
  if (!res.ok) {
    const text = await res.text()
    // Surface the API's {"error": "..."} message when present.
    let msg = text
    try {
      const parsed = JSON.parse(text)
      if (parsed && typeof parsed.error === "string") msg = parsed.error
    } catch {
      // not JSON — keep raw text
    }
    throw new Error(msg || `Request failed (${res.status})`)
  }
  return res.json()
}

export async function getSessions(): Promise<Session[]> {
  return fetchJSON<Session[]>("/api/sessions")
}

export interface SessionQuickState {
  working: boolean
  needs_input: boolean
}

export async function getSessionsQuickState(): Promise<Record<string, SessionQuickState>> {
  return fetchJSON<Record<string, SessionQuickState>>("/api/sessions/working")
}

export interface SessionResource {
  resource_type: string
  resource_id: string
  resource_url?: string
  metadata?: Record<string, string>
  unread_count: number
  unread_types?: Record<string, number>
}

export async function getSessionResources(sessionId: string): Promise<SessionResource[]> {
  return (await fetchJSON<SessionResource[] | null>(`/api/sessions/${encodeURIComponent(sessionId)}/resources`)) ?? []
}

export interface SessionCron {
  session_id: string
  job_id: string
  schedule: string
  recurring: boolean
  prompt: string
  created_at: string
  last_seen_at: string
}

export async function getSessionCrons(sessionId: string): Promise<SessionCron[]> {
  return (await fetchJSON<SessionCron[] | null>(`/api/sessions/${encodeURIComponent(sessionId)}/crons`)) ?? []
}

export interface SessionCostSummary {
  enabled: boolean
  total_cost_usd: number
  all_time_cost_usd: number
  days: { date: string; cost_usd: number; session_count: number }[]
}

export async function getSessionCost(sessionId: string): Promise<SessionCostSummary> {
  return fetchJSON<SessionCostSummary>(`/api/sessions/${encodeURIComponent(sessionId)}/cost`)
}

export interface CostMonthSummary {
  label: string
  full_label: string
  cost_usd: number
  daily_breakdown: { date: string; cost_usd: number; session_count: number }[]
  top_sessions: { session_id: string; session_name: string; cost_usd: number; input_tokens: number; output_tokens: number }[]
}

export interface CostSummary {
  enabled: boolean
  today_cost_usd: number
  all_time_cost_usd: number
  months: CostMonthSummary[]
}

export async function getCostSummary(): Promise<CostSummary> {
  return fetchJSON<CostSummary>("/api/cost")
}

export interface ArchivedSessionsParams {
  limit?: number
  offset?: number
  search?: string
  sort?: string
}

export interface ArchivedSessionsResponse {
  sessions: Session[]
  total: number
  has_more: boolean
}

export async function getArchivedSessions(params: ArchivedSessionsParams = {}): Promise<ArchivedSessionsResponse> {
  const searchParams = new URLSearchParams()
  if (params.limit) searchParams.set("limit", String(params.limit))
  if (params.offset) searchParams.set("offset", String(params.offset))
  if (params.search) searchParams.set("search", params.search)
  if (params.sort) searchParams.set("sort", params.sort)
  const qs = searchParams.toString()
  return fetchJSON<ArchivedSessionsResponse>(`/api/sessions/archived${qs ? `?${qs}` : ""}`)
}

export async function getSessionPeek(id: string): Promise<PeekState> {
  return fetchJSON<PeekState>(`/api/sessions/${encodeURIComponent(id)}/peek`)
}

export async function getSessionInbox(id: string): Promise<Event[]> {
  return (await fetchJSON<Event[] | null>(`/api/sessions/${encodeURIComponent(id)}/inbox`)) ?? []
}

export async function getCapabilities(): Promise<Capabilities> {
  return fetchJSON<Capabilities>("/api/capabilities")
}

export async function switchSession(sessionId: string): Promise<ActionResponse> {
  return fetchJSON<ActionResponse>("/api/actions/switch", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId }),
  })
}

export async function forcePeek(sessionId: string): Promise<Record<string, unknown>> {
  return fetchJSON<Record<string, unknown>>("/api/actions/peek", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId }),
  })
}

export async function dismissInbox(sessionId: string): Promise<ActionResponse> {
  return fetchJSON<ActionResponse>("/api/actions/dismiss-inbox", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId }),
  })
}

export async function dismissEvent(sessionId: string, eventId: string): Promise<ActionResponse> {
  return fetchJSON<ActionResponse>("/api/actions/dismiss-event", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId, event_id: eventId }),
  })
}

export async function addReminder(sessionId: string, title: string): Promise<ActionResponse> {
  return fetchJSON<ActionResponse>("/api/actions/add-reminder", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId, title }),
  })
}

export async function subscribeResource(sessionId: string, input: string): Promise<ActionResponse> {
  return fetchJSON<ActionResponse>("/api/actions/subscribe", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId, input }),
  })
}

export async function unsubscribeResource(sessionId: string, resourceType: string, resourceId: string): Promise<ActionResponse> {
  return fetchJSON<ActionResponse>("/api/actions/unsubscribe", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId, resource_type: resourceType, resource_id: resourceId }),
  })
}

export async function archiveSessions(sessionIds: string[]): Promise<ActionResponse> {
  return fetchJSON<ActionResponse>("/api/actions/archive-sessions", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_ids: sessionIds }),
  })
}

export interface EventsParams {
  before?: string
  limit?: number
  session?: string
  resource?: string
  type?: string
  source?: string
  search?: string
}

export async function getEvents(params: EventsParams = {}): Promise<EventsResponse> {
  const searchParams = new URLSearchParams()
  if (params.before) searchParams.set("before", params.before)
  if (params.limit) searchParams.set("limit", String(params.limit))
  if (params.session) searchParams.set("session", params.session)
  if (params.resource) searchParams.set("resource", params.resource)
  if (params.type) searchParams.set("type", params.type)
  if (params.source) searchParams.set("source", params.source)
  if (params.search) searchParams.set("search", params.search)
  const qs = searchParams.toString()
  return fetchJSON<EventsResponse>(`/api/events${qs ? `?${qs}` : ""}`)
}

export async function getResources(): Promise<ResourcesResponse> {
  return fetchJSON<ResourcesResponse>("/api/resources")
}
