import type { Session } from '../api/types';
import { timeAgo } from '../utils/time';

interface SessionCardProps {
  session: Session;
  showBranch: boolean;
  cmuxAvailable: boolean;
  onUnreadClick: (sessionId: string) => void;
  onSwitchClick: (sessionId: string) => void;
  isSwitching?: boolean;
}

export function SessionCard({
  session,
  showBranch,
  cmuxAvailable,
  onUnreadClick,
  onSwitchClick,
  isSwitching = false,
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
      ? 'var(--color-success)'
      : display_state === 'idle'
        ? 'var(--color-warning)'
        : 'var(--color-danger)';

  return (
    <div
      className={`rounded-md p-3.5 transition-all duration-200 cursor-pointer border
        ${needs_input
          ? 'bg-warning/15 border-warning'
          : 'bg-bg-secondary border-bg-tertiary hover:bg-[#1c2640] hover:border-accent'
        }`}
    >
      <div className="flex justify-between items-start gap-3 mb-2 max-[480px]:flex-col max-[480px]:items-stretch">
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <span
            className="w-2 h-2 rounded-full shrink-0"
            style={{ backgroundColor: stateColor }}
            title={display_state}
          />
          <span className="font-semibold text-[0.95rem] text-text-primary whitespace-nowrap overflow-hidden text-ellipsis">
            {session_name}
          </span>
          <span className="text-xs text-text-secondary lowercase shrink-0">
            {display_state}
          </span>
          {needs_input && <span className="text-base shrink-0">✋</span>}
        </div>

        <div className="flex gap-2 items-center shrink-0 max-[480px]:justify-end">
          {unread_count > 0 && (
            <button
              className="bg-accent text-text-primary border-none rounded-xl px-2.5 py-1 text-xs font-semibold cursor-pointer transition-all duration-200 hover:bg-accent/85 hover:scale-105"
              onClick={() => onUnreadClick(session_id)}
              title="View unread events"
            >
              {unread_count}
            </button>
          )}
          {cmuxAvailable && (
            <button
              className="bg-bg-tertiary text-text-primary border border-bg-tertiary rounded px-3 py-1.5 text-xs cursor-pointer transition-all duration-200 hover:not-disabled:bg-accent hover:not-disabled:border-accent disabled:opacity-50 disabled:cursor-not-allowed"
              onClick={() => onSwitchClick(session_id)}
              title="Switch to this session"
              disabled={isSwitching}
            >
              {isSwitching ? 'Switching...' : 'Switch'}
            </button>
          )}
        </div>
      </div>

      <div className="flex gap-3 items-center text-xs text-text-secondary mb-2 max-[480px]:flex-wrap">
        {showBranch && (
          <span className="font-mono bg-bg-primary px-2 py-0.5 rounded-sm text-xs">
            {branch}
          </span>
        )}
        <span className="whitespace-nowrap">{timeAgo(last_prompt)}</span>
      </div>

      {subscriptions_count > 0 && (
        <div className="mt-2 max-[480px]:mt-1">
          <span className="inline-block bg-bg-primary text-text-secondary px-2.5 py-1 rounded-xl text-xs">
            {subscriptions_count} resource{subscriptions_count !== 1 ? 's' : ''}
          </span>
        </div>
      )}
    </div>
  );
}
