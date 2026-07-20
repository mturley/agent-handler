import { useSessions } from '../hooks/useSessions';
import { TopBar } from '../components/TopBar';
import './SessionsPage.css';

export function SessionsPage() {
  const {
    sessions,
    sortedSessions,
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

        {/* Session cards will be rendered here in Task 7 */}
      </div>
    </div>
  );
}
