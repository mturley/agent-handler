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
  { value: 'cmux', label: 'Match cmux' },
  { value: 'last_prompt', label: 'Last prompt' },
  { value: 'created', label: 'Created' },
  { value: 'unread_count', label: 'Unread count' },
  { value: 'cost', label: 'Cost' },
  { value: 'name', label: 'Name (A-Z)' },
];

const FILTER_ACTIVE_COLORS: Record<FilterChip, string> = {
  active: 'bg-success text-text-primary border-success',
  idle: 'bg-warning text-bg-primary border-warning',
  dead: 'bg-danger text-text-primary border-danger',
  needs_input: 'bg-warning text-bg-primary border-warning',
  has_unread: 'bg-accent text-text-primary border-accent',
  blocked: 'bg-danger text-text-primary border-danger',
};

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
    <div className="flex flex-col gap-4 p-4 bg-bg-secondary border-b border-bg-tertiary">
      {/* Search and controls row */}
      <div className="flex gap-3 items-center flex-wrap max-[480px]:flex-col max-[480px]:items-stretch">
        <input
          type="text"
          className="flex-1 min-w-[200px] max-[480px]:min-w-0 max-[480px]:w-full px-3 py-2 bg-bg-primary text-text-primary border border-bg-tertiary rounded text-[0.9rem] placeholder:text-text-secondary focus:outline-none focus:border-accent"
          placeholder="Search sessions..."
          value={searchQuery}
          onChange={(e) => onSearchChange(e.target.value)}
        />

        <button
          className={`px-4 py-2 border rounded cursor-pointer text-[0.9rem] transition-all duration-200
            ${groupByRepo
              ? 'bg-accent text-text-primary border-accent'
              : 'bg-bg-primary text-text-secondary border-bg-tertiary hover:bg-bg-tertiary'
            }`}
          onClick={onToggleGrouping}
          title={groupByRepo ? 'Ungroup sessions' : 'Group by repo'}
        >
          Group
        </button>

        <div className="flex gap-0.5 max-[480px]:-order-1">
          <select
            className="px-3 py-2 bg-bg-primary text-text-primary border border-bg-tertiary rounded-l cursor-pointer text-[0.9rem] focus:outline-none focus:border-accent"
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
            className="px-3 py-2 bg-bg-primary text-text-secondary border border-bg-tertiary border-l-0 rounded-r cursor-pointer text-base leading-none transition-all duration-200 hover:bg-bg-tertiary hover:text-text-primary"
            onClick={onToggleSortDirection}
            title={sortDirection === 'asc' ? 'Ascending' : 'Descending'}
          >
            {sortDirection === 'asc' ? '↑' : '↓'}
          </button>
        </div>

        {/* Repo filter (only shown when multiple repos) */}
        {availableRepos.length > 1 && (
          <select
            className="px-3 py-2 bg-bg-primary text-text-primary border border-bg-tertiary rounded cursor-pointer text-[0.9rem] focus:outline-none focus:border-accent"
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
      <div className="flex gap-2 flex-wrap overflow-x-auto pb-1 max-[480px]:overflow-x-scroll max-[480px]:flex-nowrap">
        {FILTER_CHIPS.map((chip) => (
          <button
            key={chip.id}
            className={`px-3.5 py-1.5 border rounded-2xl cursor-pointer text-[0.85rem] whitespace-nowrap transition-all duration-200
              ${activeFilters.has(chip.id)
                ? FILTER_ACTIVE_COLORS[chip.id]
                : 'bg-bg-primary text-text-secondary border-bg-tertiary hover:bg-bg-tertiary'
              }`}
            onClick={() => onToggleFilter(chip.id)}
          >
            {chip.label}
          </button>
        ))}
      </div>
    </div>
  );
}
