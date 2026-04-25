import { useState, useEffect, useRef } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { fetchRecipeImage, previewImageByTitle } from '../api/client';
import { useInventory } from '../hooks/useInventory';
import { useEatCounts } from '../hooks/useEatCounts';
import { matchIngredients, stockSummary } from '../utils/inventoryMatch';
import AddToPlanMenu from './AddToPlanMenu';

export default function RecipeCard({ recipe: initialRecipe, showLink = false, showIngredients = false, showInstructions = false, onDelete, fetchImageEndpoint }) {
  const [recipe, setRecipe] = useState(initialRecipe);
  const [fetchingImage, setFetchingImage] = useState(false);
  const [showPlanMenu, setShowPlanMenu] = useState(false);
  const addToPlanBtnRef = useRef(null);
  const navigate = useNavigate();
  const inventory = useInventory();
  const matched = matchIngredients(recipe.ingredients, inventory);
  const summary = stockSummary(matched);
  // Eat history for the badge — only meaningful for saved recipes (have an id).
  const eatCounts = useEatCounts();
  const stats = recipe.id ? eatCounts[recipe.id] : null;

  // Auto-fetch image on mount when missing.
  // Saved recipes (have id): fetch and store via the normal endpoint.
  // Unsaved recipes (no id, e.g. ReviewPanel): fetch a remote preview by title.
  useEffect(() => {
    if (recipe.image_url) return;
    if (recipe.id) {
      fetchRecipeImage(recipe.id)
        .then(result => setRecipe(r => ({ ...r, image_url: result.image_url })))
        .catch(() => {});
    } else if (recipe.title) {
      previewImageByTitle(recipe.title)
        .then(data => { if (data?.image_url) setRecipe(r => ({ ...r, image_url: data.image_url })); })
        .catch(() => {});
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const handleFetchImage = async (e) => {
    e.preventDefault();
    setRecipe(r => ({ ...r, image_url: '' }));
    setFetchingImage(true);
    try {
      // Allow callers to override the endpoint (e.g. pending recipes use /api/pending/{id}/fetch-image).
      const result = fetchImageEndpoint
        ? await fetch(`/api${fetchImageEndpoint}`, { method: 'POST' }).then(r => r.json())
        : await fetchRecipeImage(recipe.id);
      setRecipe(r => ({ ...r, image_url: result.image_url }));
    } catch (err) {
      console.error('Image fetch failed:', err);
    } finally {
      setFetchingImage(false);
    }
  };

  const handleCardClick = () => {
    if (showLink && recipe.id) navigate(`/recipe/${recipe.id}`);
  };

  return (
    <div className="recipe-card" onClick={handleCardClick} style={showLink && recipe.id ? { cursor: 'pointer' } : undefined}>
      {recipe.image_url && (
        <img className="recipe-card-image" src={recipe.image_url} alt={recipe.title} loading="lazy" />
      )}
      <div className="recipe-card-header">
        <h3>{recipe.title}</h3>
        {recipe.cuisine_type && <span className="cuisine-badge">{recipe.cuisine_type}</span>}
      </div>
      <p className="recipe-description">{recipe.description}</p>
      <div className="recipe-meta">
        {recipe.prep_time_minutes > 0 && <span>{'\u23F1'} Prep: {recipe.prep_time_minutes}m</span>}
        {recipe.cook_time_minutes > 0 && <span>{'\uD83D\uDD25'} Cook: {recipe.cook_time_minutes}m</span>}
        <span>{'\uD83C\uDF7D'} Servings: {recipe.servings}</span>
        {recipe.difficulty && <span className={`difficulty difficulty-${recipe.difficulty}`}>{recipe.difficulty}</span>}
        {summary && (
          <span className={`stock-badge ${summary.missing === 0 ? 'stock-badge-all' : summary.inStock === 0 ? 'stock-badge-none' : 'stock-badge-partial'}`}>
            {summary.missing === 0 ? 'all in stock' : `${summary.missing} missing`}
          </span>
        )}
        {stats && stats.count > 0 && (
          <span className="eat-badge" title={
            stats.last_cooked_at
              ? `Last cooked ${new Date(stats.last_cooked_at).toLocaleDateString()}`
              : `Cooked ${stats.count}×`
          }>
            {'🍽'} {stats.count}{stats.count === 1 ? '' : '×'}
            {stats.avg_rating != null && (
              <span className="eat-badge-rating"> · ⭐ {stats.avg_rating.toFixed(1)}</span>
            )}
          </span>
        )}
      </div>
      {showIngredients && recipe.ingredients && recipe.ingredients.length > 0 && (
        <div className="recipe-card-ingredients">
          <h4>Ingredients</h4>
          <ul className="ingredients-list">
            {recipe.ingredients.map((ing, i) => (
              <li key={i} className="ingredient-row">
                <span><strong>{ing.amount} {ing.unit}</strong> {ing.name}
                {ing.notes && <span className="ingredient-notes"> ({ing.notes})</span>}</span>
                {matched && <span className={`stock-dot ${matched[i].inStock ? 'stock-dot-in' : 'stock-dot-out'}`} title={matched[i].inStock ? 'In stock' : 'Not in stock'} />}
              </li>
            ))}
          </ul>
        </div>
      )}
      {showInstructions && recipe.instructions && recipe.instructions.length > 0 && (
        <div className="recipe-card-instructions">
          <h4>Instructions</h4>
          <ol className="instructions-list">
            {recipe.instructions.map((step, i) => (
              <li key={i}>{step}</li>
            ))}
          </ol>
        </div>
      )}
      {recipe.tags && recipe.tags.length > 0 && (
        <div className="recipe-tags">
          {recipe.tags.map(tag => <span key={tag} className="tag">{tag}</span>)}
        </div>
      )}
      <div className="recipe-card-actions" onClick={e => e.stopPropagation()}>
        {showLink && recipe.id && <Link to={`/recipe/${recipe.id}`} className="btn btn-secondary">View</Link>}
        {recipe.id && (
          <button
            ref={addToPlanBtnRef}
            className="btn btn-secondary"
            onClick={() => setShowPlanMenu(true)}
          >
            + Plan
          </button>
        )}
        {recipe.id && (
          <button className="btn btn-secondary" onClick={handleFetchImage} disabled={fetchingImage}>
            {fetchingImage ? 'Fetching...' : 'Refresh Image'}
          </button>
        )}
        {onDelete && <button className="btn btn-danger" onClick={() => onDelete(recipe.id)}>Delete</button>}
      </div>
      {showPlanMenu && (
        <AddToPlanMenu
          recipeId={recipe.id}
          anchorEl={addToPlanBtnRef.current}
          onClose={() => setShowPlanMenu(false)}
        />
      )}
    </div>
  );
}
