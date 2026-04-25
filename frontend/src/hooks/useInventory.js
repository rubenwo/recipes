import { useState, useEffect } from 'react';
import { listInventory } from '../api/client';

// Module-level singleton so all components share a single fetch per session.
let _cache = null;
let _fetching = false;
const _listeners = new Set();

function loadInventory(force = false) {
  if (!force && (_cache !== null || _fetching)) return;
  _fetching = true;
  listInventory()
    .then(data => {
      _cache = data || [];
      _listeners.forEach(fn => fn(_cache));
    })
    .catch(() => {
      _cache = [];
      _listeners.forEach(fn => fn(_cache));
    })
    .finally(() => { _fetching = false; });
}

// Call after any mutation (create/update/delete/scan-approval) to refresh
// the cache so RecipeCard/RecipeDetail "in stock" badges reflect reality
// without a full page reload.
export function revalidateInventory() {
  loadInventory(true);
}

export function useInventory() {
  const [inventory, setInventory] = useState(_cache ?? []);

  useEffect(() => {
    _listeners.add(setInventory);
    if (_cache !== null) {
      setInventory(_cache);
    } else {
      loadInventory();
    }
    return () => _listeners.delete(setInventory);
  }, []);

  return inventory;
}
