import { useState, useEffect } from 'react';
import { fetchSessionInbox, postDismissInbox, postSwitch } from '../api/client';
import type { Event } from '../api/types';
import { ConfirmModal } from './ConfirmModal';
import { useToast } from '../hooks/useToast';
import { timeAgo } from '../utils/time';

interface InboxModalProps {
  isOpen: boolean;
  sessionId: string;
  sessionName: string;
  onClose: () => void;
  onRefetch: () => void;
}

export function InboxModal({
  isOpen,
  sessionId,
  sessionName,
  onClose,
  onRefetch,
}: InboxModalProps) {
  const [events, setEvents] = useState<Event[]>([]);
  const [loading, setLoading] = useState(false);
  const [expandedEvents, setExpandedEvents] = useState<Set<string>>(new Set());
  const [showConfirm, setShowConfirm] = useState(false);
  const [dismissing, setDismissing] = useState(false);
  const { showToast } = useToast();

  useEffect(() => {
    if (isOpen) {
      setLoading(true);
      fetchSessionInbox(sessionId)
        .then((data) => {
          setEvents(data);
          setLoading(false);
        })
        .catch((err) => {
          console.error('Failed to fetch inbox:', err);
          showToast('Failed to load inbox events', 'error');
          setLoading(false);
        });
    }
  }, [isOpen, sessionId, showToast]);

  const toggleExpanded = (eventId: string) => {
    setExpandedEvents((prev) => {
      const next = new Set(prev);
      if (next.has(eventId)) {
        next.delete(eventId);
      } else {
        next.add(eventId);
      }
      return next;
    });
  };

  const handleGoToSession = async () => {
    try {
      await postSwitch(sessionId);
      showToast(`Switched to session ${sessionName}`, 'success');
      onClose();
    } catch (err) {
      console.error('Failed to switch session:', err);
      showToast('Failed to switch session', 'error');
    }
  };

  const handleDismissAll = async () => {
    setDismissing(true);
    try {
      await postDismissInbox(sessionId);
      showToast(`Dismissed ${events.length} event(s) from ${sessionName}`, 'success');
      setShowConfirm(false);
      onClose();
      onRefetch();
    } catch (err) {
      console.error('Failed to dismiss inbox:', err);
      showToast('Failed to dismiss events', 'error');
    } finally {
      setDismissing(false);
    }
  };

  if (!isOpen) return null;

  const getEventTypeBadge = (type: string) => {
    const typeMap: Record<string, { label: string; color: string }> = {
      milestone: { label: 'Milestone', color: 'var(--color-accent)' },
      message: { label: 'Message', color: '#a855f7' },
      pr_comment: { label: 'PR Comment', color: 'var(--color-success)' },
      pr_review: { label: 'PR Review', color: 'var(--color-success)' },
      jira_comment: { label: 'Jira Comment', color: '#f59e0b' },
      jira_status: { label: 'Jira Status', color: '#f59e0b' },
      watcher_pr: { label: 'Watcher (PR)', color: 'var(--color-accent)' },
      watcher_jira: { label: 'Watcher (Jira)', color: '#f59e0b' },
    };

    const config = typeMap[type] || { label: type, color: 'var(--color-text-secondary)' };
    return (
      <span
        className="px-2 py-0.5 rounded text-xs font-semibold text-white whitespace-nowrap"
        style={{ backgroundColor: config.color }}
      >
        {config.label}
      </span>
    );
  };

  return (
    <>
      <div
        className="fixed inset-0 bg-black/70 flex items-center justify-center z-[999] p-5"
        onClick={onClose}
      >
        <div
          className="bg-bg-secondary border border-[#333] rounded-lg p-6 max-w-[700px] max-h-[80vh] w-full shadow-[0_8px_16px_rgba(0,0,0,0.4)] flex flex-col max-[600px]:max-w-none max-[600px]:max-h-[90vh]"
          onClick={(e) => e.stopPropagation()}
        >
          <div className="flex justify-between items-center mb-4">
            <h2 className="text-xl font-semibold text-text-primary">
              Inbox: {sessionName}
            </h2>
            <button
              className="bg-transparent border-none text-text-secondary text-2xl cursor-pointer p-0 w-8 h-8 flex items-center justify-center rounded transition-colors duration-200 hover:bg-white/10"
              onClick={onClose}
            >
              ✕
            </button>
          </div>

          {loading ? (
            <div className="py-10 px-5 text-center text-text-secondary">
              Loading events...
            </div>
          ) : events.length === 0 ? (
            <div className="py-10 px-5 text-center text-text-secondary">
              No unread events
            </div>
          ) : (
            <>
              <div className="overflow-y-auto max-h-[50vh] mb-4 flex flex-col gap-3 max-[600px]:max-h-[60vh]">
                {events.map((event) => {
                  const isExpanded = expandedEvents.has(event.ID);
                  return (
                    <div
                      key={event.ID}
                      className="bg-bg-primary border border-[#333] rounded-md p-3"
                    >
                      <div className="flex items-center gap-2 mb-1.5">
                        {getEventTypeBadge(event.Type)}
                        <span className="font-medium text-text-primary flex-1">
                          {event.Title}
                        </span>
                      </div>
                      <div className="flex gap-3 text-[0.85rem] text-text-secondary mb-2">
                        <span>{timeAgo(event.TS)}</span>
                        {event.Author && <span>by {event.Author}</span>}
                      </div>
                      {event.Body && (
                        <div className="mt-2">
                          <button
                            className="bg-transparent border-none text-accent cursor-pointer py-1 px-0 text-[0.85rem] transition-opacity duration-200 hover:opacity-80"
                            onClick={() => toggleExpanded(event.ID)}
                          >
                            {isExpanded ? '▼' : '▶'} {isExpanded ? 'Hide' : 'Show'}{' '}
                            details
                          </button>
                          {isExpanded && (
                            <pre className="mt-2 p-3 bg-black/30 rounded text-[0.85rem] text-text-secondary overflow-x-auto whitespace-pre-wrap break-words">
                              {event.Body}
                            </pre>
                          )}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>

              <div className="p-3 bg-bg-primary border-l-[3px] border-l-accent rounded mb-4 text-[0.9rem] text-text-secondary">
                <strong>
                  <span
                    className="text-accent cursor-pointer underline hover:opacity-80"
                    onClick={handleGoToSession}
                  >
                    Go to the session
                  </span>
                </strong>{' '}
                and type /inbox to deliver these, or use /inbox-mode auto to deliver them
                automatically.
              </div>

              <div className="flex justify-end">
                <button
                  className="px-4 py-2 rounded-md border-none bg-danger text-white text-[0.9rem] cursor-pointer transition-opacity duration-200 hover:not-disabled:opacity-80 disabled:opacity-50 disabled:cursor-not-allowed"
                  onClick={() => setShowConfirm(true)}
                  disabled={dismissing}
                >
                  {dismissing ? 'Dismissing...' : 'Dismiss all'}
                </button>
              </div>
            </>
          )}
        </div>
      </div>

      <ConfirmModal
        isOpen={showConfirm}
        title="Dismiss Inbox Events"
        message={`Dismiss ${events.length} unread event(s) without delivering them to ${sessionName}?`}
        onConfirm={handleDismissAll}
        onCancel={() => setShowConfirm(false)}
      />
    </>
  );
}
