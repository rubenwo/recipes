import { useMemo, useState } from 'react';
import RecipeCard from '../components/RecipeCard';
import RecipeList from '../components/RecipeList';
import { useRecipes } from '../hooks/useRecipes';
import { filterRecipes } from '../utils/fuzzyMatch';

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

export default function LibraryPage() {
  const { recipes, total, loading, error, remove } = useRecipes();
  const [query, setQuery] = useState('');
  const [expandedCuisines, setExpandedCuisines] = useState(new Set());

  const filtered = filterRecipes(query, recipes);

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

  const displayCount = query ? filtered.length : total;

  return (
    <div className="library-page">
      <h2>Recipe Library</h2>
      <div className="search-bar">
        <input
          type="text"
          placeholder="Search recipes..."
          value={query}
          onChange={e => setQuery(e.target.value)}
        />
      </div>
      {total > 0 && (
        <p className="total-count">
          {query ? `${filtered.length} of ${total}` : total} recipe{displayCount !== 1 ? 's' : ''}
        </p>
      )}
      {error && <div className="error-message">{error}</div>}
      {loading ? (
        <p>Loading...</p>
      ) : query ? (
        <RecipeList recipes={filtered} onDelete={remove} />
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
