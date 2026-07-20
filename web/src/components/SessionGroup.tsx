import './SessionGroup.css';
import type { Session } from '../api/types';
import { SessionCard } from './SessionCard';

interface SessionGroupProps {
  repo: string;
  workspace: string;
  sessions: Session[];
  cmuxAvailable: boolean;
  onUnreadClick: (sessionId: string) => void;
  onSwitchClick: (sessionId: string) => void;
}

export function SessionGroup({
  repo,
  workspace,
  sessions,
  cmuxAvailable,
  onUnreadClick,
  onSwitchClick,
}: SessionGroupProps) {
  // Check if all sessions share the same branch
  const branches = new Set(sessions.map((s) => s.branch));
  const sharedBranch = branches.size === 1 ? Array.from(branches)[0] : null;

  // Get workspace color (from the first session with a color, or fallback)
  const workspaceColor = sessions.find((s) => s.cmux_workspace_color)?.cmux_workspace_color || '#a855f7';

  return (
    <div className="session-group">
      <div className="session-group-repo-header">{repo}</div>

      <div className="session-group-workspace">
        <div className="session-group-workspace-header">
          <div
            className="session-group-workspace-bar"
            style={{ backgroundColor: workspaceColor }}
          />
          <div className="session-group-workspace-info">
            <span className="session-group-workspace-name">{workspace}</span>
            {sharedBranch && <span className="session-group-branch">{sharedBranch}</span>}
          </div>
        </div>

        <div className="session-group-cards">
          {sessions.map((session) => (
            <SessionCard
              key={session.session_id}
              session={session}
              showBranch={!sharedBranch}
              cmuxAvailable={cmuxAvailable}
              onUnreadClick={onUnreadClick}
              onSwitchClick={onSwitchClick}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
