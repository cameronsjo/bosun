import { useState, useEffect, useCallback, useRef } from 'react';
import { APIError } from '../lib/api';

interface UsePollingOptions<T> {
  fetcher: () => Promise<T>;
  interval?: number; // milliseconds
  enabled?: boolean;
}

interface UsePollingResult<T> {
  data: T | null;
  error: APIError | null;
  isLoading: boolean;
  isOffline: boolean;
  lastUpdated: Date | null;
  refetch: () => Promise<void>;
}

export function usePolling<T>({
  fetcher,
  interval = 5000,
  enabled = true,
}: UsePollingOptions<T>): UsePollingResult<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<APIError | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);

  const fetcherRef = useRef(fetcher);

  useEffect(() => {
    fetcherRef.current = fetcher;
  }, [fetcher]);

  const fetchData = useCallback(async () => {
    try {
      const result = await fetcherRef.current();
      setData(result);
      setError(null);
      setLastUpdated(new Date());
    } catch (err) {
      if (err instanceof APIError) {
        setError(err);
      } else {
        setError(new APIError(String(err), 0));
      }
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!enabled) return;

    // Initial fetch
    fetchData();

    // Set up polling
    const intervalId = setInterval(fetchData, interval);

    return () => clearInterval(intervalId);
  }, [enabled, interval, fetchData]);

  const isOffline = error !== null && !error.isAuthError;

  return {
    data,
    error,
    isLoading,
    isOffline,
    lastUpdated,
    refetch: fetchData,
  };
}
