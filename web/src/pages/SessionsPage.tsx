import { useSessions } from '../hooks/useSessions';
import { useCapabilities } from '../hooks/useCapabilities';
import { TopBar } from '../components/TopBar';
import { SessionGroup } from '../components/SessionGroup';
import { SessionCard } from '../components/SessionCard';
import './SessionsPage.css';

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
  } = useSessions();

  const { capabilities } = useCapabilities();
  const cmuxAvailable = capabilities?.cmux ?? false;

  // Placeholder handlers for Task 8
  const handleUnreadClick = (sessionId: string) => {
    console.log('Open inbox modal for session:', sessionId);
  };

  const handleSwitchClick = (sessionId: string) => {
    console.log('Switch to session:', sessionId);
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
      <div className="sessions-page">
        <div className="sessions-loading">Loading sessions...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="sessions-page">
        <div className="sessions-error">Error loading sessions: {error.message}</div>
      </div>
    );
  }

  return (
    <div className="sessions-page">
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

      <div className="sessions-content">
        <div className="sessions-count">
          Showing {sortedSessions.length} of {sessions.length} sessions
        </div>

        {groupByRepo ? (
          <div className="sessions-grouped">
            {sortedGroups.map((group) => (
              <SessionGroup
                key={`${group.repo}::${group.workspace}`}
                repo={group.repo}
                workspace={group.workspace}
                sessions={group.sessions}
                cmuxAvailable={cmuxAvailable}
                onUnreadClick={handleUnreadClick}
                onSwitchClick={handleSwitchClick}
              />
            ))}
          </div>
        ) : (
          <div className="sessions-flat">
            {sortedSessions.map((session) => (
              <SessionCard
                key={session.session_id}
                session={session}
                showBranch={true}
                cmuxAvailable={cmuxAvailable}
                onUnreadClick={handleUnreadClick}
                onSwitchClick={handleSwitchClick}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
