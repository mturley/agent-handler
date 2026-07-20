export interface Session {
  session_id: string;
  session_name: string;
  branch: string;
  repo: string;
  display_state: string;
  inbox_mode: string;
  peekable: boolean;
  terminal_type: string;
  unread_count: number;
  unread_breakdown: Record<string, number>;
  last_active: string;
  last_prompt: string;
  cmux_workspace: string;
  cmux_workspace_color: string;
  needs_input: boolean;
  subscriptions_count: number;
}

export interface PeekState {
  session_id: string;
  content: string;
  needs_input: boolean;
  reason: string;
  updated_at: string;
}

export interface Event {
  ID: string;
  TS: string;
  Source: string;
  SessionID: string | null;
  Type: string;
  Title: string;
  Body: string | null;
  Author: string | null;
  Broadcast: boolean;
}

export interface Capabilities {
  cmux: boolean;
}
