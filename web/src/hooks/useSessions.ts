import { useState, useEffect, useMemo, useCallback } from 'react';
import { fetchSessions } from '../api/client';
import { useSSE } from './useSSE';
import type { Session } from '../api/types';

export type SortOption = 'last_prompt' | 'created' | 'unread_count' | 'cost' | 'name';
export type SortDirection = 'asc' | 'desc';
export type FilterChip = 'active' | 'idle' | 'dead' | 'needs_input' | 'has_unread' | 'blocked';

export interface GroupedSessions {
  repo: string;
  workspace: string;
  sessions: Session[];
}

export function useSessions() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  // Search and filter state
  const [searchQuery, setSearchQuery] = useState('');
  const [activeFilters, setActiveFilters] = useState<Set<FilterChip>>(new Set());
  const [selectedRepo, setSelectedRepo] = useState<string | null>(null);

  // Sort state
  const [sortOption, setSortOption] = useState<SortOption>('last_prompt');
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc');

  // Grouping state
  const [groupByRepo, setGroupByRepo] = useState(true);

  // Fetch sessions
  const loadSessions = useCallback(async () => {
    try {
      setLoading(true);
      const data = await fetchSessions();
      setSessions(data);
      setError(null);
    } catch (err) {
      setError(err as Error);
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial load
  useEffect(() => {
    loadSessions();
  }, [loadSessions]);

  // Refetch on SSE heartbeat
  useSSE(loadSessions);

  // Filter chips toggle
  const toggleFilter = useCallback((filter: FilterChip) => {
    setActiveFilters((prev) => {
      const next = new Set(prev);
      if (next.has(filter)) {
        next.delete(filter);
      } else {
        next.add(filter);
      }
      return next;
    });
  }, []);

  // Clear all filters
  const clearFilters = useCallback(() => {
    setActiveFilters(new Set());
    setSelectedRepo(null);
    setSearchQuery('');
  }, []);

  // Fuzzy search helper (matches on session_name AND branch)
  const matchesSearch = useCallback((session: Session, query: string): boolean => {
    if (!query) return true;
    const lowerQuery = query.toLowerCase();
    const nameMatch = session.session_name.toLowerCase().includes(lowerQuery);
    const branchMatch = session.branch.toLowerCase().includes(lowerQuery);
    return nameMatch || branchMatch;
  }, []);

  // Filter helper
  const matchesFilters = useCallback((session: Session): boolean => {
    if (activeFilters.size === 0 && !selectedRepo) return true;

    // Check repo filter
    if (selectedRepo && session.repo !== selectedRepo) {
      return false;
    }

    // If no chip filters, only repo filter was applied
    if (activeFilters.size === 0) return true;

    // Check chip filters (any match = include)
    let matches = false;
    if (activeFilters.has('active') && session.display_state === 'active') matches = true;
    if (activeFilters.has('idle') && session.display_state === 'idle') matches = true;
    if (activeFilters.has('dead') && session.display_state === 'dead') matches = true;
    if (activeFilters.has('needs_input') && session.needs_input) matches = true;
    if (activeFilters.has('has_unread') && session.unread_count > 0) matches = true;
    if (activeFilters.has('blocked')) {
      // Blocked sessions are those with unread_breakdown containing 'blocked' type
      const blockedCount = session.unread_breakdown?.blocked || 0;
      if (blockedCount > 0) matches = true;
    }

    return matches;
  }, [activeFilters, selectedRepo]);

  // Filtered sessions (after search + filters)
  const filteredSessions = useMemo(() => {
    return sessions.filter((s) => matchesSearch(s, searchQuery) && matchesFilters(s));
  }, [sessions, searchQuery, matchesSearch, matchesFilters]);

  // Sorted sessions
  const sortedSessions = useMemo(() => {
    const sorted = [...filteredSessions];
    sorted.sort((a, b) => {
      let comparison = 0;

      switch (sortOption) {
        case 'last_prompt':
          comparison = new Date(a.last_prompt).getTime() - new Date(b.last_prompt).getTime();
          break;
        case 'created':
          // Assuming session_id contains timestamp or we use last_active as proxy
          comparison = new Date(a.last_active).getTime() - new Date(b.last_active).getTime();
          break;
        case 'unread_count':
          comparison = a.unread_count - b.unread_count;
          break;
        case 'cost':
          // TODO: Add cost field to Session type when available
          comparison = 0;
          break;
        case 'name':
          comparison = a.session_name.localeCompare(b.session_name);
          break;
      }

      return sortDirection === 'asc' ? comparison : -comparison;
    });

    return sorted;
  }, [filteredSessions, sortOption, sortDirection]);

  // Grouped sessions (by repo + workspace)
  const groupedSessions = useMemo((): GroupedSessions[] => {
    if (!groupByRepo) return [];

    const groups = new Map<string, GroupedSessions>();

    sortedSessions.forEach((session) => {
      const key = `${session.repo}::${session.cmux_workspace || 'default'}`;
      if (!groups.has(key)) {
        groups.set(key, {
          repo: session.repo,
          workspace: session.cmux_workspace || 'default',
          sessions: [],
        });
      }
      groups.get(key)!.sessions.push(session);
    });

    return Array.from(groups.values());
  }, [sortedSessions, groupByRepo]);

  // Available repos for filter dropdown
  const availableRepos = useMemo(() => {
    const repos = new Set<string>();
    sessions.forEach((s) => repos.add(s.repo));
    return Array.from(repos).sort();
  }, [sessions]);

  // Toggle sort direction
  const toggleSortDirection = useCallback(() => {
    setSortDirection((prev) => (prev === 'asc' ? 'desc' : 'asc'));
  }, []);

  return {
    // Data
    sessions,
    filteredSessions,
    sortedSessions,
    groupedSessions,
    loading,
    error,

    // Search
    searchQuery,
    setSearchQuery,

    // Filters
    activeFilters,
    toggleFilter,
    clearFilters,
    selectedRepo,
    setSelectedRepo,
    availableRepos,

    // Sort
    sortOption,
    setSortOption,
    sortDirection,
    toggleSortDirection,

    // Grouping
    groupByRepo,
    setGroupByRepo,

    // Actions
    refresh: loadSessions,
  };
}
