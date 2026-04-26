import { useState, useEffect, useRef } from 'react';
import { fetchRecipeImage, updateRecipeContent } from '../api/client';
import RecipeEditForm from './RecipeEditForm';
import CookingChat from './CookingChat';
import RecipeHistory from './RecipeHistory';
import AddToPlanMenu from './AddToPlanMenu';
import { useInventory } from '../hooks/useInventory';
import { matchIngredients, stockSummary } from '../utils/inventoryMatch';

export default function RecipeDetail({ recipe: initialRecipe }) {
  const [recipe, setRecipe] = useState(initialRecipe);
  const inventory = useInventory();
  const matched = matchIngredients(recipe.ingredients, inventory);
  const summary = stockSummary(matched);
  const [fetchingImage, setFetchingImage] = useState(false);
  const [editing, setEditing] = useState(false);
  const [editSaving, setEditSaving] = useState(false);
  const [showPlanMenu, setShowPlanMenu] = useState(false);
  const addToPlanBtnRef = useRef(null);

  useEffect(() => {
    if (recipe.id && !recipe.image_url) {
      fetchRecipeImage(recipe.id)
        .then(result => setRecipe(r => ({ ...r, image_url: result.image_url })))
        .catch(() => {});
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const handleRefreshImage = async () => {
    setRecipe(r => ({ ...r, image_url: '' }));
    setFetchingImage(true);
    try {
      const result = await fetchRecipeImage(recipe.id);
      setRecipe(r => ({ ...r, image_url: result.image_url }));
    } catch (err) {
      console.error('Image fetch failed:', err);
    } finally {
      setFetchingImage(false);
    }
  };

  const handleEditSave = async ({ ingredients, instructions, cuisine_type }) => {
    setEditSaving(true);
    try {
      await updateRecipeContent(recipe.id, ingredients, instructions, cuisine_type);
      setRecipe(r => ({ ...r, ingredients, instructions, cuisine_type }));
      setEditing(false);
    } catch (err) {
      alert('Failed to save: ' + err.message);
    } finally {
      setEditSaving(false);
    }
  };

  return (
    <div className="recipe-detail">
      <div className={`recipe-detail-hero${recipe.image_url ? '' : ' recipe-detail-hero--no-image'}`}>
        {recipe.image_url && (
          <img className="recipe-detail-hero-image" src={recipe.image_url} alt={recipe.title} />
        )}
        <div className="recipe-detail-hero-overlay" />
        <div className="recipe-detail-hero-content">
          {recipe.cuisine_type && <span className="cuisine-badge">{recipe.cuisine_type}</span>}
          <h2>{recipe.title}</h2>
          {recipe.description && (
            <p style={{ fontSize: 17, lineHeight: 1.5, maxWidth: '60ch', opacity: 0.9 }}>
              {recipe.description}
            </p>
          )}
          <div className="recipe-detail-hero-meta">
            {recipe.prep_time_minutes > 0 && <span>Prep {recipe.prep_time_minutes} min</span>}
            {recipe.cook_time_minutes > 0 && <span>Cook {recipe.cook_time_minutes} min</span>}
            <span>{recipe.servings} servings</span>
            {recipe.difficulty && <span className={`difficulty difficulty-${recipe.difficulty}`}>{recipe.difficulty}</span>}
          </div>
        </div>
      </div>

      {editing ? (
        <div className="recipe-detail-section">
          <RecipeEditForm
            recipe={recipe}
            onSave={handleEditSave}
            onCancel={() => setEditing(false)}
            saving={editSaving}
          />
        </div>
      ) : (
        <div className="recipe-detail-body">
          <aside className="recipe-detail-aside">
            <div className="recipe-detail-aside-card">
              <h3>At a glance</h3>
              <div className="recipe-detail-stats">
                <div className="recipe-detail-stat">
                  <span className="recipe-detail-stat-label">Prep</span>
                  <span className="recipe-detail-stat-value">{recipe.prep_time_minutes || 0}m</span>
                </div>
                <div className="recipe-detail-stat">
                  <span className="recipe-detail-stat-label">Cook</span>
                  <span className="recipe-detail-stat-value">{recipe.cook_time_minutes || 0}m</span>
                </div>
                <div className="recipe-detail-stat">
                  <span className="recipe-detail-stat-label">Serves</span>
                  <span className="recipe-detail-stat-value">{recipe.servings || 0}</span>
                </div>
              </div>
            </div>

            <div className="recipe-detail-aside-card">
              <div className="recipe-section-header">
                <h3 style={{ flex: 1 }}>Ingredients</h3>
                {summary && (
                  <span className={`stock-badge ${summary.missing === 0 ? 'stock-badge-all' : summary.inStock === 0 ? 'stock-badge-none' : 'stock-badge-partial'}`}>
                    {summary.missing === 0 ? 'all in stock' : `${summary.missing} missing`}
                  </span>
                )}
              </div>
              <ul className="ingredients-list">
                {recipe.ingredients && recipe.ingredients.map((ing, i) => (
                  <li key={i} className="ingredient-row">
                    <span><strong>{ing.amount} {ing.unit}</strong> {ing.name}
                    {ing.notes && <span className="ingredient-notes"> ({ing.notes})</span>}</span>
                    {matched && <span className={`stock-dot ${matched[i].inStock ? 'stock-dot-in' : 'stock-dot-out'}`} title={matched[i].inStock ? 'In stock' : 'Not in stock'} />}
                  </li>
                ))}
              </ul>
            </div>

            {recipe.dietary_restrictions && recipe.dietary_restrictions.length > 0 && (
              <div className="recipe-detail-aside-card">
                <h3>Dietary</h3>
                <div style={{ display: 'flex', flexWrap: 'wrap' }}>
                  {recipe.dietary_restrictions.map(d => <span key={d} className="tag dietary-tag">{d}</span>)}
                </div>
              </div>
            )}

            {recipe.id && (
              <div className="recipe-detail-actions">
                <button
                  ref={addToPlanBtnRef}
                  className="btn btn-primary"
                  onClick={() => setShowPlanMenu(v => !v)}
                >
                  + Add to plan
                </button>
                <button className="btn btn-secondary" onClick={() => setEditing(true)}>
                  Edit recipe
                </button>
                <button className="btn btn-secondary" onClick={handleRefreshImage} disabled={fetchingImage}>
                  {fetchingImage ? 'Fetching…' : 'Refresh image'}
                </button>
                {showPlanMenu && (
                  <AddToPlanMenu
                    recipeId={recipe.id}
                    anchorEl={addToPlanBtnRef.current}
                    onClose={() => setShowPlanMenu(false)}
                  />
                )}
              </div>
            )}
          </aside>

          <div className="recipe-detail-main">
            <section className="recipe-detail-section">
              <h3>Method</h3>
              <ol className="instructions-list">
                {recipe.instructions && recipe.instructions.map((step, i) => (
                  <li key={i}>{step}</li>
                ))}
              </ol>
              {recipe.tags && recipe.tags.length > 0 && (
                <div className="recipe-tags">
                  {recipe.tags.map(tag => <span key={tag} className="tag">{tag}</span>)}
                </div>
              )}
              {recipe.generated_by_model && (
                <p className="recipe-model">Generated by {recipe.generated_by_model}</p>
              )}
            </section>

            {recipe.id && (
              <section className="recipe-detail-section">
                <h3>Cook history</h3>
                <RecipeHistory recipeId={recipe.id} />
              </section>
            )}

            {recipe.id && (
              <section className="recipe-detail-section" style={{ padding: 0 }}>
                <CookingChat recipeId={recipe.id} />
              </section>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
