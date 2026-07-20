import type { Session, PeekState, Event, Capabilities } from './types';

const API_BASE = '/api';

async function fetchJSON<T>(path: string): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`);
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}: ${response.statusText}`);
  }
  return response.json();
}

async function postJSON(path: string, body?: unknown): Promise<void> {
  const response = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}: ${response.statusText}`);
  }
}

export async function fetchSessions(): Promise<Session[]> {
  return fetchJSON<Session[]>('/sessions');
}

export async function fetchSession(id: string): Promise<Session> {
  return fetchJSON<Session>(`/sessions/${id}`);
}

export async function fetchSessionPeek(id: string): Promise<PeekState> {
  return fetchJSON<PeekState>(`/sessions/${id}/peek`);
}

export async function fetchSessionInbox(id: string): Promise<Event[]> {
  return fetchJSON<Event[]>(`/sessions/${id}/inbox`);
}

export async function fetchCapabilities(): Promise<Capabilities> {
  return fetchJSON<Capabilities>('/capabilities');
}

export async function postSwitch(sessionId: string): Promise<void> {
  return postJSON('/actions/switch', { session_id: sessionId });
}

export async function postForcePeek(sessionId: string): Promise<any> {
  const response = await fetch(`${API_BASE}/actions/peek`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id: sessionId }),
  });
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}: ${response.statusText}`);
  }
  return response.json();
}

export async function postDismissInbox(sessionId: string): Promise<void> {
  return postJSON('/actions/dismiss-inbox', { session_id: sessionId });
}
