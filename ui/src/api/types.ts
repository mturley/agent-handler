export type DisplayState = "active" | "idle" | "dead" | "archived"

export interface Session {
  session_id: string
  session_name: string
  branch: string
  repo: string
  display_state: DisplayState
  inbox_mode: string
  peekable: boolean
  terminal_type?: string
  unread_count: number
  unread_breakdown?: Record<string, number>
  last_active: string
  last_prompt?: string
  cmux_workspace?: string
  cmux_workspace_color?: string
  needs_input: boolean
  pid: number
  status: string
  subscriptions_count: number
  subscriptions_breakdown?: Record<string, number>
  cwd?: string
  cmux_order: number
}

export interface PeekState {
  content: string
  needs_input: boolean
  reason: string
  updated_at: string
}

export interface Event {
  id: string
  ts: string
  external_ts?: string
  source: string
  session_id?: string
  type: string
  title: string
  body?: string
  author?: string
  author_type?: string
  broadcast: boolean
  tags?: string
}

export interface Capabilities {
  cmux: boolean
}

export interface ActionResponse {
  success: boolean
  output?: string
}

export interface EventResource {
  resource_type: string
  resource_id: string
  resource_url?: string
  metadata?: Record<string, string>
}

export interface TimelineEvent extends Event {
  session_name: string
  resources: EventResource[]
}

export interface EventsResponse {
  events: TimelineEvent[]
  has_more: boolean
  next_cursor: string
}
