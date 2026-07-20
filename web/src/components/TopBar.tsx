import './TopBar.css';
import type { SortOption, SortDirection, FilterChip } from '../hooks/useSessions';

interface TopBarProps {
  searchQuery: string;
  onSearchChange: (query: string) => void;
  sortOption: SortOption;
  onSortOptionChange: (option: SortOption) => void;
  sortDirection: SortDirection;
  onToggleSortDirection: () => void;
  activeFilters: Set<FilterChip>;
  onToggleFilter: (filter: FilterChip) => void;
  groupByRepo: boolean;
  onToggleGrouping: () => void;
  availableRepos: string[];
  selectedRepo: string | null;
  onSelectRepo: (repo: string | null) => void;
}

const FILTER_CHIPS: { id: FilterChip; label: string }[] = [
  { id: 'active', label: 'Active' },
  { id: 'idle', label: 'Idle' },
  { id: 'dead', label: 'Dead' },
  { id: 'needs_input', label: 'Needs input' },
  { id: 'has_unread', label: 'Has unread' },
  { id: 'blocked', label: 'Blocked' },
];

const SORT_OPTIONS: { value: SortOption; label: string }[] = [
  { value: 'last_prompt', label: 'Last prompt' },
  { value: 'created', label: 'Created' },
  { value: 'unread_count', label: 'Unread count' },
  { value: 'cost', label: 'Cost' },
  { value: 'name', label: 'Name (A-Z)' },
];

export function TopBar({
  searchQuery,
  onSearchChange,
  sortOption,
  onSortOptionChange,
  sortDirection,
  onToggleSortDirection,
  activeFilters,
  onToggleFilter,
  groupByRepo,
  onToggleGrouping,
  availableRepos,
  selectedRepo,
  onSelectRepo,
}: TopBarProps) {
  return (
    <div className="top-bar">
      {/* Search and controls row */}
      <div className="top-bar-controls">
        <input
          type="text"
          className="search-input"
          placeholder="Search sessions..."
          value={searchQuery}
          onChange={(e) => onSearchChange(e.target.value)}
        />

        <button
          className={`group-toggle ${groupByRepo ? 'active' : ''}`}
          onClick={onToggleGrouping}
          title={groupByRepo ? 'Ungroup sessions' : 'Group by repo'}
        >
          Group
        </button>

        <div className="sort-control">
          <select
            className="sort-select"
            value={sortOption}
            onChange={(e) => onSortOptionChange(e.target.value as SortOption)}
          >
            {SORT_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
          <button
            className="sort-direction"
            onClick={onToggleSortDirection}
            title={sortDirection === 'asc' ? 'Ascending' : 'Descending'}
          >
            {sortDirection === 'asc' ? '↑' : '↓'}
          </button>
        </div>

        {/* Repo filter (only shown when multiple repos) */}
        {availableRepos.length > 1 && (
          <select
            className="repo-filter"
            value={selectedRepo || ''}
            onChange={(e) => onSelectRepo(e.target.value || null)}
          >
            <option value="">All repos</option>
            {availableRepos.map((repo) => (
              <option key={repo} value={repo}>
                {repo}
              </option>
            ))}
          </select>
        )}
      </div>

      {/* Filter chips row */}
      <div className="filter-chips">
        {FILTER_CHIPS.map((chip) => (
          <button
            key={chip.id}
            className={`filter-chip ${chip.id} ${activeFilters.has(chip.id) ? 'active' : ''}`}
            onClick={() => onToggleFilter(chip.id)}
          >
            {chip.label}
          </button>
        ))}
      </div>
    </div>
  );
}
