import type { Session, PeekState, Event, Capabilities, ActionResponse, EventsResponse, ResourcesResponse } from "./types"

const BASE = ""

async function fetchJSON<T>(url: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${url}`, options)
  if (!res.ok) {
    const text = await res.text()
    throw new Error(`${res.status}: ${text}`)
  }
  return res.json()
}

export async function getSessions(): Promise<Session[]> {
  return fetchJSON<Session[]>("/api/sessions")
}

export interface ArchivedSessionsParams {
  limit?: number
  offset?: number
  search?: string
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
  const qs = searchParams.toString()
  return fetchJSON<ArchivedSessionsResponse>(`/api/sessions/archived${qs ? `?${qs}` : ""}`)
}

export async function getSessionPeek(id: string): Promise<PeekState> {
  return fetchJSON<PeekState>(`/api/sessions/${encodeURIComponent(id)}/peek`)
}

export async function getSessionInbox(id: string): Promise<Event[]> {
  return fetchJSON<Event[]>(`/api/sessions/${encodeURIComponent(id)}/inbox`)
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
  type?: string
  source?: string
  search?: string
}

export async function getEvents(params: EventsParams = {}): Promise<EventsResponse> {
  const searchParams = new URLSearchParams()
  if (params.before) searchParams.set("before", params.before)
  if (params.limit) searchParams.set("limit", String(params.limit))
  if (params.session) searchParams.set("session", params.session)
  if (params.type) searchParams.set("type", params.type)
  if (params.source) searchParams.set("source", params.source)
  if (params.search) searchParams.set("search", params.search)
  const qs = searchParams.toString()
  return fetchJSON<EventsResponse>(`/api/events${qs ? `?${qs}` : ""}`)
}

export async function getResources(): Promise<ResourcesResponse> {
  return fetchJSON<ResourcesResponse>("/api/resources")
}
