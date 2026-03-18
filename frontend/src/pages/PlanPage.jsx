import { useState, useEffect, useCallback } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import {
  getPlan, createPlan, listPlans, deletePlan,
  addRecipeToPlan, removeRecipeFromPlan, updatePlanRecipe,
  listRecipes, updatePlan, getPlanIngredients, getPlanSuggestions,
  saveRecipe, randomizePlan,
} from '../api/client';
import RecipeCard from '../components/RecipeCard';
import { filterRecipes } from '../utils/fuzzyMatch';

export default function PlanPage() {
  const { id } = useParams();

  if (id) return <PlanDetail planId={parseInt(id)} />;
  return <PlansList />;
}

function PlansList() {
  const [plans, setPlans] = useState([]);
  const [name, setName] = useState('');
  const [loading, setLoading] = useState(true);
  const navigate = useNavigate();

  useEffect(() => {
    listPlans().then(data => { setPlans(data || []); setLoading(false); });
  }, []);

  const handleCreate = async (e) => {
    e.preventDefault();
    if (!name.trim()) return;
    const plan = await createPlan(name.trim());
    setName('');
    navigate(`/plans/${plan.id}`);
  };

  const handleDelete = async (id) => {
    await deletePlan(id);
    setPlans(prev => prev.filter(p => p.id !== id));
  };

  const statusLabel = (s) => ({ draft: 'Draft', active: 'Active', completed: 'Done' }[s] || s);

  return (
    <div className="plans-page">
      <h2>Meal Plans</h2>
      <form onSubmit={handleCreate} className="plan-create-form">
        <input
          type="text"
          placeholder="New plan name (e.g. Week 12)"
          value={name}
          onChange={e => setName(e.target.value)}
        />
        <button className="btn btn-primary" type="submit" disabled={!name.trim()}>Create Plan</button>
      </form>

      {loading ? (
        <p className="empty-state">Loading...</p>
      ) : plans.length === 0 ? (
        <p className="empty-state">No meal plans yet. Create one above.</p>
      ) : (
        <div className="plans-list">
          {plans.map(p => (
            <div key={p.id} className="plan-list-item">
              <div className="plan-list-info">
                <Link to={`/plans/${p.id}`} className="plan-list-name">{p.name}</Link>
                <span className={`plan-status plan-status-${p.status}`}>{statusLabel(p.status)}</span>
              </div>
              <button className="btn btn-danger" onClick={() => handleDelete(p.id)}>Delete</button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function PlanDetail({ planId }) {
  const [plan, setPlan] = useState(null);
  const [recipes, setRecipes] = useState([]);
  const [servingsInput, setServingsInput] = useState({});
  const [loading, setLoading] = useState(true);
  const [adding, setAdding] = useState({});
  const [ingredients, setIngredients] = useState([]);
  const [recipeFilter, setRecipeFilter] = useState('');
  const [randomDays, setRandomDays] = useState([4]);
  const [randomizing, setRandomizing] = useState(false);
  const [loadingIngredients, setLoadingIngredients] = useState(true);

  const loadPlan = useCallback(async () => {
    const data = await getPlan(planId);
    setPlan(data);
    setLoading(false);
  }, [planId]);

  const loadIngredients = useCallback(async () => {
    setLoadingIngredients(true);
    try {
      const data = await getPlanIngredients(planId);
      setIngredients(data || []);
    } catch { /* ignore if plan has no recipes */ }
    finally { setLoadingIngredients(false); }
  }, [planId]);

  useEffect(() => { loadPlan(); loadIngredients(); }, [loadPlan, loadIngredients]);

  useEffect(() => {
    listRecipes(500, 0).then(data => setRecipes(data.recipes || []));
  }, []);

  const planRecipeIds = new Set((plan?.recipes || []).map(r => r.recipe_id));

  const updatePlanAndIngredients = (updated) => {
    setPlan(updated);
    loadIngredients();
  };

  const handleAdd = async (recipeId) => {
    const servings = servingsInput[recipeId] || 4;
    setAdding(prev => ({ ...prev, [recipeId]: true }));
    try {
      const updated = await addRecipeToPlan(planId, recipeId, servings);
      updatePlanAndIngredients(updated);
    } finally {
      setAdding(prev => ({ ...prev, [recipeId]: false }));
    }
  };

  const handleRemove = async (recipeId) => {
    const updated = await removeRecipeFromPlan(planId, recipeId);
    updatePlanAndIngredients(updated);
  };

  const handleServingsChange = async (recipeId, servings) => {
    const updated = await updatePlanRecipe(planId, recipeId, { servings });
    updatePlanAndIngredients(updated);
  };

  const handleStartPeriod = async () => {
    const updated = await updatePlan(planId, { status: 'active' });
    setPlan(updated);
  };

  const handleComplete = async (recipeId, completed) => {
    const updated = await updatePlanRecipe(planId, recipeId, { completed });
    setPlan(updated);
  };

  const handleEndPeriod = async () => {
    const updated = await updatePlan(planId, { status: 'completed' });
    setPlan(updated);
  };

  const handleRandomize = async () => {
    setRandomizing(true);
    try {
      const updated = await randomizePlan(planId, randomDays);
      updatePlanAndIngredients(updated);
    } catch (err) {
      alert('Failed to randomize: ' + err.message);
    } finally {
      setRandomizing(false);
    }
  };

  if (loading) return <p className="empty-state">Loading...</p>;
  if (!plan) return <p className="empty-state">Plan not found.</p>;

  const isActive = plan.status === 'active';
  const isCompleted = plan.status === 'completed';
  const isDraft = plan.status === 'draft';
  const completedCount = (plan.recipes || []).filter(r => r.completed).length;

  return (
    <div className="plan-detail">
      <div className="plan-detail-header">
        <Link to="/plans" className="btn btn-secondary">Back</Link>
        <h2>{plan.name}</h2>
        <span className={`plan-status plan-status-${plan.status}`}>
          {plan.status === 'draft' ? 'Draft' : plan.status === 'active' ? 'Active' : 'Done'}
        </span>
      </div>

      {isActive && (
        <ActivePlanView
          plan={plan}
          onComplete={handleComplete}
          onEnd={handleEndPeriod}
          completedCount={completedCount}
        />
      )}

      {isDraft && loadingIngredients && (
        <div className="plan-section plan-loading">
          <div className="plan-loading-spinner" />
          <p>Normalizing ingredients...</p>
        </div>
      )}

      {isDraft && !loadingIngredients && (
        <>
          {/* Randomize bar */}
          <div className="plan-section plan-randomize-bar">
            <h3>Randomize</h3>
            <p className="plan-randomize-hint">Set servings per day, then randomize to auto-fill the plan.</p>
            <div className="plan-randomize-days">
              {randomDays.map((s, i) => (
                <div key={i} className="plan-randomize-day">
                  <label>Day {i + 1}</label>
                  <input
                    type="number"
                    min="1"
                    max="20"
                    value={s}
                    onChange={e => setRandomDays(prev => prev.map((v, j) => j === i ? (parseInt(e.target.value) || 1) : v))}
                  />
                  {randomDays.length > 1 && (
                    <button className="btn btn-danger btn-sm" onClick={() => setRandomDays(prev => prev.filter((_, j) => j !== i))}>
                      &times;
                    </button>
                  )}
                </div>
              ))}
            </div>
            <div className="plan-randomize-actions">
              <button className="btn btn-secondary" onClick={() => setRandomDays(prev => [...prev, prev[prev.length - 1] || 4])}>
                + Add Day
              </button>
              <button className="btn btn-primary" onClick={handleRandomize} disabled={randomizing}>
                {randomizing ? 'Randomizing...' : `Randomize ${randomDays.length} days`}
              </button>
            </div>
          </div>

          {/* Selected recipes */}
          <div className="plan-section">
            <h3>Selected Recipes ({(plan.recipes || []).length})</h3>
            {(plan.recipes || []).length === 0 ? (
              <p className="empty-state">No recipes selected yet. Add some below.</p>
            ) : (
              <>
                <div className="plan-recipes">
                  {plan.recipes.map(mpr => (
                    <div key={mpr.recipe_id} className="plan-recipe-item">
                      <RecipeCard recipe={mpr.recipe} />
                      <div className="plan-recipe-controls">
                        <label>
                          Servings:
                          <input
                            type="number"
                            min="1"
                            max="20"
                            value={mpr.servings}
                            onChange={e => handleServingsChange(mpr.recipe_id, parseInt(e.target.value) || 1)}
                          />
                        </label>
                        <button className="btn btn-danger" onClick={() => handleRemove(mpr.recipe_id)}>Remove</button>
                      </div>
                    </div>
                  ))}
                </div>
                {ingredients.length > 0 && <IngredientSummary ingredients={ingredients} planId={planId} />}
                <button className="btn btn-primary plan-start-btn" onClick={handleStartPeriod}>
                  Start Period
                </button>
              </>
            )}
          </div>

          {/* Recipe browser */}
          <div className="plan-section">
            <h3>Add Recipes</h3>
            <div className="search-bar" style={{ marginBottom: 16 }}>
              <input
                type="text"
                placeholder="Filter recipes..."
                value={recipeFilter}
                onChange={e => setRecipeFilter(e.target.value)}
              />
            </div>
            <div className="plan-recipe-browser">
              {filterRecipes(recipeFilter, recipes).filter(r => !planRecipeIds.has(r.id)).map(recipe => (
                <div key={recipe.id} className="plan-browse-item">
                  <div className="plan-browse-info">
                    <strong>{recipe.title}</strong>
                    {recipe.cuisine_type && <span className="cuisine-badge">{recipe.cuisine_type}</span>}
                    <span className="plan-browse-meta">{recipe.servings} servings</span>
                  </div>
                  <div className="plan-browse-actions">
                    <input
                      type="number"
                      min="1"
                      max="20"
                      value={servingsInput[recipe.id] || recipe.servings}
                      onChange={e => setServingsInput(prev => ({ ...prev, [recipe.id]: parseInt(e.target.value) || 1 }))}
                      className="servings-input"
                    />
                    <button
                      className="btn btn-primary"
                      onClick={() => handleAdd(recipe.id)}
                      disabled={adding[recipe.id]}
                    >
                      {adding[recipe.id] ? 'Adding...' : 'Add'}
                    </button>
                  </div>
                </div>
              ))}
              {filterRecipes(recipeFilter, recipes).filter(r => !planRecipeIds.has(r.id)).length === 0 && (
                <p className="empty-state">{recipeFilter ? 'No recipes match your filter.' : 'All recipes are already in this plan.'}</p>
              )}
            </div>
          </div>
        </>
      )}

      {isCompleted && (
        <div className="plan-section">
          <p className="empty-state">This plan is completed. All {(plan.recipes || []).length} recipes done.</p>
        </div>
      )}
    </div>
  );
}

function IngredientSummary({ ingredients, planId }) {
  const [expanded, setExpanded] = useState({});
  const [selected, setSelected] = useState({});
  const [suggestions, setSuggestions] = useState(null);
  const [loadingSuggestions, setLoadingSuggestions] = useState(false);
  const [savedGenerated, setSavedGenerated] = useState(false);

  const selectedNames = ingredients.filter((_, i) => selected[i]).map(ing => ing.name);

  const handleFindRecipes = async () => {
    if (selectedNames.length === 0) return;
    setLoadingSuggestions(true);
    setSuggestions(null);
    setSavedGenerated(false);
    try {
      const data = await getPlanSuggestions(planId, selectedNames);
      setSuggestions(data);
    } catch (err) {
      alert('Failed to get suggestions: ' + err.message);
    } finally {
      setLoadingSuggestions(false);
    }
  };

  const handleSaveGenerated = async () => {
    if (!suggestions?.generated_recipe) return;
    try {
      await saveRecipe(suggestions.generated_recipe);
      setSavedGenerated(true);
    } catch (err) {
      alert('Failed to save: ' + err.message);
    }
  };

  return (
    <div className="ingredient-summary">
      <h4>Shopping List ({ingredients.length} items)</h4>
      <p className="ingredient-summary-hint">Select leftover ingredients to find recipes that use them</p>
      <ul className="ingredient-summary-list">
        {ingredients.map((ing, i) => (
          <li key={i} className="ingredient-summary-item">
            <div className="ingredient-summary-main">
              <input
                type="checkbox"
                checked={!!selected[i]}
                onChange={() => setSelected(prev => ({ ...prev, [i]: !prev[i] }))}
              />
              <span className="ingredient-summary-amount">{ing.amount} {ing.unit}</span>
              <span className="ingredient-summary-name" onClick={() => setExpanded(prev => ({ ...prev, [i]: !prev[i] }))}>{ing.name}</span>
              <span className="ingredient-summary-count">{ing.recipes.length} recipe{ing.recipes.length > 1 ? 's' : ''}</span>
            </div>
            {expanded[i] && (
              <div className="ingredient-summary-recipes">
                {ing.recipes.map((r, j) => <span key={j} className="tag">{r}</span>)}
              </div>
            )}
          </li>
        ))}
      </ul>

      {selectedNames.length > 0 && (
        <button
          className="btn btn-primary ingredient-suggest-btn"
          onClick={handleFindRecipes}
          disabled={loadingSuggestions}
        >
          {loadingSuggestions ? 'Searching...' : `Find recipes for ${selectedNames.length} ingredient${selectedNames.length > 1 ? 's' : ''}`}
        </button>
      )}

      {suggestions && (
        <div className="suggestions-panel">
          <h4>Suggestions</h4>
          {(suggestions.db_recipes || []).length > 0 && (
            <div className="suggestions-section">
              <h5>From your library</h5>
              {suggestions.db_recipes.map(r => (
                <div key={r.id} className="suggestion-item">
                  <RecipeCard recipe={r} showLink />
                </div>
              ))}
            </div>
          )}
          {suggestions.generated_recipe && (
            <div className="suggestions-section">
              <h5>Generated recipe</h5>
              <div className="suggestion-item">
                <RecipeCard recipe={suggestions.generated_recipe} showIngredients />
                {savedGenerated ? (
                  <span className="saved-badge">Saved!</span>
                ) : (
                  <button className="btn btn-primary" onClick={handleSaveGenerated}>Save Recipe</button>
                )}
              </div>
            </div>
          )}
          {(suggestions.db_recipes || []).length === 0 && !suggestions.generated_recipe && (
            <p className="empty-state">No suggestions found for these ingredients.</p>
          )}
        </div>
      )}
    </div>
  );
}

function ActivePlanView({ plan, onComplete, onEnd, completedCount }) {
  const [currentIndex, setCurrentIndex] = useState(0);
  const recipes = plan.recipes || [];
  const total = recipes.length;

  if (total === 0) return <p className="empty-state">No recipes in this plan.</p>;

  const current = recipes[currentIndex];

  return (
    <div className="active-plan">
      <div className="active-plan-progress">
        <span>{completedCount} of {total} completed</span>
        <div className="progress-bar">
          <div className="progress-fill" style={{ width: `${(completedCount / total) * 100}%` }} />
        </div>
      </div>

      <div className="active-plan-nav">
        <button className="btn btn-secondary" onClick={() => setCurrentIndex(i => i - 1)} disabled={currentIndex === 0}>
          Previous
        </button>
        <span className="active-plan-counter">{currentIndex + 1} / {total}</span>
        <button className="btn btn-secondary" onClick={() => setCurrentIndex(i => i + 1)} disabled={currentIndex === total - 1}>
          Next
        </button>
      </div>

      <div className={`active-plan-card ${current.completed ? 'active-plan-card-done' : ''}`}>
        <RecipeCard recipe={current.recipe} showIngredients />
        <div className="active-plan-servings">
          Adjusted servings: <strong>{current.servings}</strong>
          {current.servings !== current.recipe.servings && (
            <span className="servings-note"> (original: {current.recipe.servings})</span>
          )}
        </div>
        <button
          className={`btn ${current.completed ? 'btn-secondary' : 'btn-primary'} active-plan-complete-btn`}
          onClick={() => onComplete(current.recipe_id, !current.completed)}
        >
          {current.completed ? 'Mark as Not Done' : 'Mark as Done'}
        </button>
      </div>

      {completedCount === total && (
        <button className="btn btn-primary plan-start-btn" onClick={onEnd}>
          End Period
        </button>
      )}
    </div>
  );
}
