import type { Session } from '../api/types';
import { SessionCard } from './SessionCard';

interface SessionGroupProps {
  repo: string;
  workspace: string;
  sessions: Session[];
  cmuxAvailable: boolean;
  onUnreadClick: (sessionId: string) => void;
  onSwitchClick: (sessionId: string) => void;
  switchingSessionId: string | null;
}

export function SessionGroup({
  repo,
  workspace,
  sessions,
  cmuxAvailable,
  onUnreadClick,
  onSwitchClick,
  switchingSessionId,
}: SessionGroupProps) {
  // Check if all sessions share the same branch
  const branches = new Set(sessions.map((s) => s.branch));
  const sharedBranch = branches.size === 1 ? Array.from(branches)[0] : null;

  // Get workspace color (from the first session with a color, or fallback)
  const workspaceColor = sessions.find((s) => s.cmux_workspace_color)?.cmux_workspace_color || '#a855f7';

  return (
    <div className="mb-8">
      <div className="text-lg font-bold text-text-primary mb-4 px-4 max-[480px]:text-base max-[480px]:px-2">
        {repo}
      </div>

      <div className="mb-6">
        <div className="flex items-center gap-3 mb-3 px-4 max-[480px]:px-2">
          <div
            className="w-1 h-6 rounded-sm shrink-0"
            style={{ backgroundColor: workspaceColor }}
          />
          <div className="flex items-center gap-3 flex-wrap">
            <span className="text-[0.95rem] font-semibold text-text-primary">
              {workspace}
            </span>
            {sharedBranch && (
              <span className="font-mono bg-bg-primary text-text-secondary px-2.5 py-0.5 rounded-sm text-xs">
                {sharedBranch}
              </span>
            )}
          </div>
        </div>

        <div className="flex flex-col gap-3 px-4 max-[480px]:px-2">
          {sessions.map((session) => (
            <SessionCard
              key={session.session_id}
              session={session}
              showBranch={!sharedBranch}
              cmuxAvailable={cmuxAvailable}
              onUnreadClick={onUnreadClick}
              onSwitchClick={onSwitchClick}
              isSwitching={switchingSessionId === session.session_id}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
