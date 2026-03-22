import { useEffect, useMemo, useState } from 'react';
import RecipeCard from '../components/RecipeCard';
import RecipeList from '../components/RecipeList';
import { useRecipes } from '../hooks/useRecipes';
import { filterRecipes } from '../utils/fuzzyMatch';
import { aiSearchRecipes, getRecipeSuggestions, getSettings } from '../api/client';

const CUISINE_COLORS = [
  '#c2410c', '#0d9488', '#7c3aed', '#b45309',
  '#0369a1', '#15803d', '#be123c', '#6d28d9',
  '#0f766e', '#b45309',
];

function cuisineColor(name) {
  let hash = 0;
  for (let i = 0; i < name.length; i++) hash = (hash * 31 + name.charCodeAt(i)) >>> 0;
  return CUISINE_COLORS[hash % CUISINE_COLORS.length];
}

function CuisineGroup({ cuisine, recipes, expanded, onToggle, onDelete }) {
  const previews = recipes.filter(r => r.image_url).slice(0, 4);
  const color = cuisineColor(cuisine);

  return (
    <div className={`cuisine-group${expanded ? ' cuisine-group--expanded' : ''}`}>
      <button className="cuisine-group-header" onClick={onToggle} style={{ '--cuisine-color': color }}>
        <div className="cuisine-group-info">
          <span className="cuisine-group-name">{cuisine}</span>
          <span className="cuisine-group-count">{recipes.length} recipe{recipes.length !== 1 ? 's' : ''}</span>
        </div>
        <div className="cuisine-group-right">
          {!expanded && previews.length > 0 && (
            <div className="cuisine-group-previews">
              {previews.map(r => (
                <img key={r.id} className="cuisine-preview-img" src={r.image_url} alt="" />
              ))}
            </div>
          )}
          <span className="cuisine-group-arrow">{expanded ? '▲' : '▼'}</span>
        </div>
      </button>
      {expanded && (
        <div className="cuisine-group-body">
          <div className="recipe-list">
            {recipes.map(recipe => (
              <RecipeCard key={recipe.id} recipe={recipe} showLink onDelete={onDelete} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function SuggestedCarousel({ count }) {
  const [recipes, setRecipes] = useState([]);

  useEffect(() => {
    if (!count || count < 1) return;
    getRecipeSuggestions(count)
      .then(data => setRecipes(data || []))
      .catch(() => {});
  }, [count]);

  if (recipes.length === 0) return null;

  return (
    <div className="suggestions-carousel">
      <div className="suggestions-carousel-header">
        <h3>Today's picks</h3>
        <span className="suggestions-carousel-hint">Refreshes daily · based on your library</span>
      </div>
      <div className="suggestions-carousel-track">
        {recipes.map(recipe => (
          <RecipeCard key={recipe.id} recipe={recipe} showLink />
        ))}
      </div>
    </div>
  );
}

export default function LibraryPage() {
  const { recipes, total, loading, error, remove } = useRecipes();
  const [query, setQuery] = useState('');
  const [searchMode, setSearchMode] = useState('fuzzy'); // 'fuzzy' | 'ai'
  const [aiResults, setAiResults] = useState(null); // { recipes, total, interpreted } | null
  const [aiLoading, setAiLoading] = useState(false);
  const [aiError, setAiError] = useState(null);
  const [expandedCuisines, setExpandedCuisines] = useState(new Set());
  const [suggestionCount, setSuggestionCount] = useState(3);

  useEffect(() => {
    getSettings()
      .then(data => {
        const map = {};
        (data || []).forEach(s => { map[s.key] = s.value; });
        if (map.suggestion_count) setSuggestionCount(parseInt(map.suggestion_count, 10) || 3);
      })
      .catch(() => {});
  }, []);

  // Trigger AI search on explicit submit (Enter key)
  const handleAiSearch = () => {
    if (!query.trim()) {
      setAiResults(null);
      setAiError(null);
      return;
    }
    setAiLoading(true);
    setAiError(null);
    aiSearchRecipes(query.trim())
      .then(data => {
        setAiResults(data);
        setAiLoading(false);
      })
      .catch(err => {
        setAiError(err.message || 'AI search failed');
        setAiLoading(false);
      });
  };

  // Reset AI state when switching modes
  const handleModeChange = (mode) => {
    setSearchMode(mode);
    setAiResults(null);
    setAiError(null);
    setAiLoading(false);
  };

  const fuzzyFiltered = filterRecipes(query, recipes);

  const cuisineGroups = useMemo(() => {
    const groups = {};
    recipes.forEach(r => {
      const cuisine = r.cuisine_type || 'Other';
      if (!groups[cuisine]) groups[cuisine] = [];
      groups[cuisine].push(r);
    });
    Object.values(groups).forEach(arr =>
      arr.sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
    );
    return Object.entries(groups).sort(([a], [b]) => a.localeCompare(b));
  }, [recipes]);

  const toggleCuisine = (cuisine) => {
    setExpandedCuisines(prev => {
      const next = new Set(prev);
      next.has(cuisine) ? next.delete(cuisine) : next.add(cuisine);
      return next;
    });
  };

  const isSearching = query.trim().length > 0;
  const fuzzyCount = fuzzyFiltered.length;
  const aiCount = aiResults ? aiResults.total : 0;
  const displayCount = searchMode === 'ai' ? aiCount : (isSearching ? fuzzyCount : total);

  return (
    <div className="library-page">
      <h2>Recipe Library</h2>
      <div className="search-bar">
        <input
          type="text"
          placeholder={searchMode === 'ai' ? 'Describe what you\'re looking for...' : 'Search recipes...'}
          value={query}
          onChange={e => setQuery(e.target.value)}
          onKeyDown={e => { if (searchMode === 'ai' && e.key === 'Enter') handleAiSearch(); }}
        />
      </div>
      <div className="mode-toggle">
        <button
          className={searchMode === 'fuzzy' ? 'active' : ''}
          onClick={() => handleModeChange('fuzzy')}
        >
          Keyword
        </button>
        <button
          className={searchMode === 'ai' ? 'active' : ''}
          onClick={() => handleModeChange('ai')}
        >
          AI Search
        </button>
      </div>
      {total > 0 && (
        <p className="total-count">
          {isSearching
            ? `${displayCount} of ${total}`
            : total} recipe{displayCount !== 1 ? 's' : ''}
          {aiLoading && ' · searching...'}
        </p>
      )}
      {(error || aiError) && <div className="error-message">{error || aiError}</div>}
      {searchMode === 'ai' && aiResults?.interpreted && isSearching && (
        <p className="total-count" style={{ marginBottom: 12 }}>
          {[
            aiResults.interpreted.query && `"${aiResults.interpreted.query}"`,
            aiResults.interpreted.cuisine_type && aiResults.interpreted.cuisine_type,
            ...(aiResults.interpreted.dietary_restrictions || []),
            ...(aiResults.interpreted.tags || []),
            aiResults.interpreted.max_total_minutes > 0 && `≤${aiResults.interpreted.max_total_minutes} min`,
          ].filter(Boolean).join(' · ')}
        </p>
      )}
      {!isSearching && !loading && total >= suggestionCount && (
        <SuggestedCarousel count={suggestionCount} />
      )}
      {loading ? (
        <p>Loading...</p>
      ) : searchMode === 'ai' ? (
        isSearching ? (
          aiLoading ? null : <RecipeList recipes={aiResults?.recipes || []} onDelete={remove} searchQuery={query} />
        ) : (
          <div className="cuisine-groups">
            {cuisineGroups.map(([cuisine, groupRecipes]) => (
              <CuisineGroup
                key={cuisine}
                cuisine={cuisine}
                recipes={groupRecipes}
                expanded={expandedCuisines.has(cuisine)}
                onToggle={() => toggleCuisine(cuisine)}
                onDelete={remove}
              />
            ))}
          </div>
        )
      ) : isSearching ? (
        <RecipeList recipes={fuzzyFiltered} onDelete={remove} searchQuery={query} />
      ) : (
        <div className="cuisine-groups">
          {cuisineGroups.map(([cuisine, groupRecipes]) => (
            <CuisineGroup
              key={cuisine}
              cuisine={cuisine}
              recipes={groupRecipes}
              expanded={expandedCuisines.has(cuisine)}
              onToggle={() => toggleCuisine(cuisine)}
              onDelete={remove}
            />
          ))}
        </div>
      )}
    </div>
  );
}
