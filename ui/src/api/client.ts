import type { Session, PeekState, Event, Capabilities, ActionResponse } from "./types"

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
