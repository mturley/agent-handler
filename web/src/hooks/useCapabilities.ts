import { useState, useEffect } from 'react';
import { fetchCapabilities } from '../api/client';
import type { Capabilities } from '../api/types';

export function useCapabilities() {
  const [capabilities, setCapabilities] = useState<Capabilities | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    fetchCapabilities()
      .then((caps) => {
        setCapabilities(caps);
        setLoading(false);
      })
      .catch((err) => {
        setError(err);
        setLoading(false);
      });
  }, []);

  return { capabilities, loading, error };
}
