import './SessionCard.css';
import type { Session } from '../api/types';
import { timeAgo } from '../utils/time';

interface SessionCardProps {
  session: Session;
  showBranch: boolean;
  cmuxAvailable: boolean;
  onUnreadClick: (sessionId: string) => void;
  onSwitchClick: (sessionId: string) => void;
}

export function SessionCard({
  session,
  showBranch,
  cmuxAvailable,
  onUnreadClick,
  onSwitchClick,
}: SessionCardProps) {
  const {
    session_id,
    session_name,
    branch,
    display_state,
    needs_input,
    unread_count,
    last_prompt,
    subscriptions_count,
  } = session;

  // State dot color
  const stateColor =
    display_state === 'active'
      ? 'var(--success)'
      : display_state === 'idle'
        ? 'var(--warning)'
        : 'var(--danger)';

  return (
    <div className={`session-card ${needs_input ? 'needs-input' : ''}`}>
      <div className="session-card-header">
        <div className="session-card-name-row">
          <span
            className="session-card-state-dot"
            style={{ backgroundColor: stateColor }}
            title={display_state}
          />
          <span className="session-card-name">{session_name}</span>
          <span className="session-card-state-label">{display_state}</span>
          {needs_input && <span className="session-card-needs-input-icon">✋</span>}
        </div>

        <div className="session-card-actions">
          {unread_count > 0 && (
            <button
              className="session-card-unread-badge"
              onClick={() => onUnreadClick(session_id)}
              title="View unread events"
            >
              {unread_count}
            </button>
          )}
          {cmuxAvailable && (
            <button
              className="session-card-switch-btn"
              onClick={() => onSwitchClick(session_id)}
              title="Switch to this session"
            >
              Switch
            </button>
          )}
        </div>
      </div>

      <div className="session-card-meta">
        {showBranch && <span className="session-card-branch">{branch}</span>}
        <span className="session-card-time">{timeAgo(last_prompt)}</span>
      </div>

      {subscriptions_count > 0 && (
        <div className="session-card-resources">
          <span className="session-card-resource-count">
            {subscriptions_count} resource{subscriptions_count !== 1 ? 's' : ''}
          </span>
        </div>
      )}
    </div>
  );
}
