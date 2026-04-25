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
  const [activeTab, setActiveTab] = useState('overview');
  const [showPlanMenu, setShowPlanMenu] = useState(false);
  const addToPlanBtnRef = useRef(null);

  // Auto-fetch image on mount if missing
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

  const tab = (name) => `recipe-tab-panel${activeTab === name ? ' mobile-tab-panel-active' : ''}`;

  return (
    <div className="recipe-detail">
      <div className="recipe-detail-header">
        <div className="recipe-detail-title-row">
          <h2>{recipe.title}</h2>
          {recipe.cuisine_type && <span className="cuisine-badge">{recipe.cuisine_type}</span>}
          {recipe.id && (
            <button
              ref={addToPlanBtnRef}
              className="btn btn-secondary btn-sm recipe-detail-add-plan-btn"
              onClick={() => setShowPlanMenu(v => !v)}
            >
              + Add to plan
            </button>
          )}
          {showPlanMenu && (
            <AddToPlanMenu
              recipeId={recipe.id}
              anchorEl={addToPlanBtnRef.current}
              onClose={() => setShowPlanMenu(false)}
            />
          )}
        </div>
        <p className="recipe-description">{recipe.description}</p>
        <div className="recipe-meta">
          {recipe.prep_time_minutes > 0 && <span>{'\u23F1'} Prep: {recipe.prep_time_minutes} min</span>}
          {recipe.cook_time_minutes > 0 && <span>{'\uD83D\uDD25'} Cook: {recipe.cook_time_minutes} min</span>}
          <span>{'\uD83C\uDF7D'} Servings: {recipe.servings}</span>
          {recipe.difficulty && <span className={`difficulty difficulty-${recipe.difficulty}`}>{recipe.difficulty}</span>}
        </div>
      </div>

      {!editing && (
        <div className="mobile-tabs" role="tablist">
          {['overview', 'ingredients', 'steps'].map(t => (
            <button
              key={t}
              role="tab"
              aria-selected={activeTab === t}
              className={`mobile-tab${activeTab === t ? ' mobile-tab-active' : ''}`}
              onClick={() => setActiveTab(t)}
            >
              {t === 'overview' ? 'Overview' : t === 'ingredients' ? 'Ingredients' : 'Steps'}
            </button>
          ))}
          {recipe.id && (
            <button
              role="tab"
              aria-selected={activeTab === 'history'}
              className={`mobile-tab${activeTab === 'history' ? ' mobile-tab-active' : ''}`}
              onClick={() => setActiveTab('history')}
            >
              History
            </button>
          )}
          {recipe.id && (
            <button
              role="tab"
              aria-selected={activeTab === 'chat'}
              className={`mobile-tab${activeTab === 'chat' ? ' mobile-tab-active' : ''}`}
              onClick={() => setActiveTab('chat')}
            >
              Chef
            </button>
          )}
        </div>
      )}

      {/* Overview: image + dietary */}
      <div className={tab('overview')}>
        {recipe.image_url && (
          <img className="recipe-detail-image" src={recipe.image_url} alt={recipe.title} />
        )}
        {recipe.id && (
          <div className="recipe-detail-image-action">
            <button className="btn btn-secondary btn-sm" onClick={handleRefreshImage} disabled={fetchingImage}>
              {fetchingImage ? 'Fetching…' : 'Refresh Image'}
            </button>
          </div>
        )}
        {recipe.dietary_restrictions && recipe.dietary_restrictions.length > 0 && (
          <div className="recipe-dietary">
            {recipe.dietary_restrictions.map(d => <span key={d} className="tag dietary-tag">{d}</span>)}
          </div>
        )}
      </div>

      {/* Ingredients + Steps (side-by-side grid on desktop, separate tabs on mobile) */}
      {editing ? (
        <RecipeEditForm
          recipe={recipe}
          onSave={handleEditSave}
          onCancel={() => setEditing(false)}
          saving={editSaving}
        />
      ) : (
        <div className="recipe-content-area">
          <div className={tab('ingredients')}>
            <section className="recipe-section recipe-ingredients">
              <div className="recipe-section-header">
                <h3>Ingredients</h3>
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
            </section>
            {recipe.id && (
              <div className="recipe-edit-trigger">
                <button className="btn btn-secondary btn-sm" onClick={() => setEditing(true)}>
                  Edit ingredients &amp; instructions
                </button>
              </div>
            )}
          </div>

          <div className={tab('steps')}>
            <section className="recipe-section recipe-instructions">
              <div className="recipe-section-header">
                <h3>Instructions</h3>
              </div>
              <ol className="instructions-list">
                {recipe.instructions && recipe.instructions.map((step, i) => (
                  <li key={i}>{step}</li>
                ))}
              </ol>
            </section>
            {recipe.tags && recipe.tags.length > 0 && (
              <div className="recipe-tags">
                {recipe.tags.map(tag => <span key={tag} className="tag">{tag}</span>)}
              </div>
            )}
            {recipe.generated_by_model && (
              <p className="recipe-model">Generated by: {recipe.generated_by_model}</p>
            )}
          </div>
        </div>
      )}

      {/* History tab */}
      {recipe.id && (
        <div className={tab('history')}>
          <section className="recipe-section">
            <div className="recipe-section-header">
              <h3>Cook history</h3>
            </div>
            <RecipeHistory recipeId={recipe.id} />
          </section>
        </div>
      )}

      {/* Chat tab */}
      {recipe.id && (
        <div className={tab('chat')}>
          <CookingChat recipeId={recipe.id} />
        </div>
      )}
    </div>
  );
}
