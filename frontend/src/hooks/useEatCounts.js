import { useState, useEffect } from 'react';
import { listRecipeEatCounts } from '../api/client';

// Module-level singleton: one fetch per session, shared by every RecipeCard.
// Mirrors the useInventory pattern.
let _cache = null;
let _fetching = false;
const _listeners = new Set();

function loadEatCounts(force = false) {
  if (!force && (_cache !== null || _fetching)) return;
  _fetching = true;
  listRecipeEatCounts()
    .then(arr => {
      // Index by recipe_id for O(1) lookups in RecipeCard.
      const byId = {};
      for (const s of (arr || [])) byId[s.recipe_id] = s;
      _cache = byId;
      _listeners.forEach(fn => fn(_cache));
    })
    .catch(() => {
      _cache = {};
      _listeners.forEach(fn => fn(_cache));
    })
    .finally(() => { _fetching = false; });
}

// Call after marking a recipe Done / changing a rating / ending a plan
// so the library badges reflect the new state without a page reload.
export function revalidateEatCounts() {
  loadEatCounts(true);
}

export function useEatCounts() {
  const [counts, setCounts] = useState(_cache ?? {});

  useEffect(() => {
    _listeners.add(setCounts);
    if (_cache !== null) {
      setCounts(_cache);
    } else {
      loadEatCounts();
    }
    return () => _listeners.delete(setCounts);
  }, []);

  return counts;
}
