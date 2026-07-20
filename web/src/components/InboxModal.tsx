import { useState, useEffect } from 'react';
import './InboxModal.css';
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
      milestone: { label: 'Milestone', color: 'var(--accent)' },
      message: { label: 'Message', color: '#a855f7' },
      pr_comment: { label: 'PR Comment', color: 'var(--success)' },
      pr_review: { label: 'PR Review', color: 'var(--success)' },
      jira_comment: { label: 'Jira Comment', color: '#f59e0b' },
      jira_status: { label: 'Jira Status', color: '#f59e0b' },
      watcher_pr: { label: 'Watcher (PR)', color: 'var(--accent)' },
      watcher_jira: { label: 'Watcher (Jira)', color: '#f59e0b' },
    };

    const config = typeMap[type] || { label: type, color: 'var(--text-secondary)' };
    return (
      <span className="event-type-badge" style={{ backgroundColor: config.color }}>
        {config.label}
      </span>
    );
  };

  return (
    <>
      <div className="modal-backdrop" onClick={onClose}>
        <div className="modal-card inbox-modal" onClick={(e) => e.stopPropagation()}>
          <div className="inbox-modal-header">
            <h2 className="modal-title">Inbox: {sessionName}</h2>
            <button className="inbox-close-btn" onClick={onClose}>
              ✕
            </button>
          </div>

          {loading ? (
            <div className="inbox-loading">Loading events...</div>
          ) : events.length === 0 ? (
            <div className="inbox-empty">No unread events</div>
          ) : (
            <>
              <div className="inbox-events">
                {events.map((event) => {
                  const isExpanded = expandedEvents.has(event.ID);
                  return (
                    <div key={event.ID} className="inbox-event">
                      <div className="inbox-event-header">
                        {getEventTypeBadge(event.Type)}
                        <span className="inbox-event-title">{event.Title}</span>
                      </div>
                      <div className="inbox-event-meta">
                        <span className="inbox-event-time">{timeAgo(event.TS)}</span>
                        {event.Author && (
                          <span className="inbox-event-author">by {event.Author}</span>
                        )}
                      </div>
                      {event.Body && (
                        <div className="inbox-event-body-section">
                          <button
                            className="inbox-event-toggle"
                            onClick={() => toggleExpanded(event.ID)}
                          >
                            {isExpanded ? '▼' : '▶'} {isExpanded ? 'Hide' : 'Show'} details
                          </button>
                          {isExpanded && (
                            <pre className="inbox-event-body">{event.Body}</pre>
                          )}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>

              <div className="inbox-instructions">
                <strong>
                  <span className="inbox-link" onClick={handleGoToSession}>
                    Go to the session
                  </span>
                </strong>{' '}
                and type /inbox to deliver these, or use /inbox-mode auto to deliver them
                automatically.
              </div>

              <div className="inbox-actions">
                <button
                  className="inbox-dismiss-btn"
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
