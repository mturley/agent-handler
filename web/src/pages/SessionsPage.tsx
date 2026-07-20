import { useState } from 'react';
import { useSessions } from '../hooks/useSessions';
import { useCapabilities } from '../hooks/useCapabilities';
import { TopBar } from '../components/TopBar';
import { SessionGroup } from '../components/SessionGroup';
import { SessionCard } from '../components/SessionCard';
import { InboxModal } from '../components/InboxModal';
import { useToast } from '../hooks/useToast';
import { postSwitch } from '../api/client';

export function SessionsPage() {
  const {
    sessions,
    sortedSessions,
    groupedSessions,
    loading,
    error,
    searchQuery,
    setSearchQuery,
    activeFilters,
    toggleFilter,
    selectedRepo,
    setSelectedRepo,
    availableRepos,
    sortOption,
    setSortOption,
    sortDirection,
    toggleSortDirection,
    groupByRepo,
    setGroupByRepo,
    refresh,
  } = useSessions();

  const { capabilities } = useCapabilities();
  const cmuxAvailable = capabilities?.cmux ?? false;
  const { showToast } = useToast();

  const [inboxModalOpen, setInboxModalOpen] = useState(false);
  const [inboxSessionId, setInboxSessionId] = useState<string>('');
  const [inboxSessionName, setInboxSessionName] = useState<string>('');
  const [switchingSessionId, setSwitchingSessionId] = useState<string | null>(null);

  const handleUnreadClick = (sessionId: string) => {
    const session = sessions.find((s) => s.session_id === sessionId);
    if (session) {
      setInboxSessionId(sessionId);
      setInboxSessionName(session.session_name);
      setInboxModalOpen(true);
    }
  };

  const handleSwitchClick = async (sessionId: string) => {
    const session = sessions.find((s) => s.session_id === sessionId);
    if (!session) return;

    setSwitchingSessionId(sessionId);
    try {
      await postSwitch(sessionId);
      showToast(`Switched to session ${session.session_name}`, 'success');
    } catch (err) {
      console.error('Failed to switch session:', err);
      showToast('Failed to switch session', 'error');
    } finally {
      setSwitchingSessionId(null);
    }
  };

  // Sort groups by their top-ranked member
  const sortedGroups = [...groupedSessions].sort((a, b) => {
    const aTopSession = a.sessions[0];
    const bTopSession = b.sessions[0];
    const aIndex = sortedSessions.indexOf(aTopSession);
    const bIndex = sortedSessions.indexOf(bTopSession);
    return aIndex - bIndex;
  });

  if (loading) {
    return (
      <div className="flex flex-col h-full">
        <div className="flex justify-center items-center h-full text-text-secondary text-base">
          Loading sessions...
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col h-full">
        <div className="flex justify-center items-center h-full text-danger text-base">
          Error loading sessions: {error.message}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full">
      <TopBar
        searchQuery={searchQuery}
        onSearchChange={setSearchQuery}
        sortOption={sortOption}
        onSortOptionChange={setSortOption}
        sortDirection={sortDirection}
        onToggleSortDirection={toggleSortDirection}
        activeFilters={activeFilters}
        onToggleFilter={toggleFilter}
        groupByRepo={groupByRepo}
        onToggleGrouping={() => setGroupByRepo(!groupByRepo)}
        availableRepos={availableRepos}
        selectedRepo={selectedRepo}
        onSelectRepo={setSelectedRepo}
      />

      <div className="flex-1 overflow-y-auto p-4 max-[480px]:p-2">
        <div className="text-text-secondary text-[0.9rem] mb-4 px-4 max-[480px]:px-2">
          Showing {sortedSessions.length} of {sessions.length} sessions
        </div>

        {groupByRepo ? (
          <div className="mt-4">
            {sortedGroups.map((group) => (
              <SessionGroup
                key={`${group.repo}::${group.workspace}`}
                repo={group.repo}
                workspace={group.workspace}
                sessions={group.sessions}
                cmuxAvailable={cmuxAvailable}
                onUnreadClick={handleUnreadClick}
                onSwitchClick={handleSwitchClick}
                switchingSessionId={switchingSessionId}
              />
            ))}
          </div>
        ) : (
          <div className="flex flex-col gap-3 mt-4 px-4 max-[480px]:px-2">
            {sortedSessions.map((session) => (
              <SessionCard
                key={session.session_id}
                session={session}
                showBranch={true}
                cmuxAvailable={cmuxAvailable}
                onUnreadClick={handleUnreadClick}
                onSwitchClick={handleSwitchClick}
                isSwitching={switchingSessionId === session.session_id}
              />
            ))}
          </div>
        )}
      </div>

      <InboxModal
        isOpen={inboxModalOpen}
        sessionId={inboxSessionId}
        sessionName={inboxSessionName}
        onClose={() => setInboxModalOpen(false)}
        onRefetch={refresh}
      />
    </div>
  );
}
