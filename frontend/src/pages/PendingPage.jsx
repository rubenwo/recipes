import { useState, useEffect, useRef } from 'react';
import {
  listPendingRecipes, approvePendingRecipe, rejectPendingRecipe,
  getSettings, updatePendingRecipeContent, getFeatureStatus,
  previewImageByTitle,
} from '../api/client';
import RecipeEditForm from '../components/RecipeEditForm';

function PendingThumb({ recipe }) {
  const [src, setSrc] = useState(recipe.image_url || '');

  useEffect(() => {
    if (src || !recipe.title) return;
    let cancelled = false;
    previewImageByTitle(recipe.title)
      .then(data => { if (!cancelled && data?.image_url) setSrc(data.image_url); })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [recipe.title, src]);

  if (src) {
    return <img className="pending-item-thumb" src={src} alt={recipe.title} loading="lazy" />;
  }
  const letter = (recipe.title || '?').trim().charAt(0).toUpperCase();
  return <div className="pending-item-thumb-placeholder" aria-hidden="true">{letter}</div>;
}

function PendingItem({ recipe, onApprove, onReject, onEdit, editing, onEditSave, onEditCancel, editSaving, acting }) {
  const [expanded, setExpanded] = useState(false);

  if (editing) {
    return (
      <div className="pending-item">
        <div className="pending-item-expanded" style={{ borderTop: 'none' }}>
          <RecipeEditForm
            recipe={recipe}
            onSave={onEditSave}
            onCancel={onEditCancel}
            saving={editSaving}
          />
        </div>
      </div>
    );
  }

  return (
    <div className="pending-item">
      <div className="pending-item-row" onClick={() => setExpanded(e => !e)}>
        <PendingThumb recipe={recipe} />
        <div className="pending-item-body">
          <div className="pending-item-title-row">
            <span className="pending-item-title">{recipe.title}</span>
            {recipe.cuisine_type && <span className="cuisine-badge">{recipe.cuisine_type}</span>}
          </div>
          {recipe.description && (
            <p className="pending-item-desc">{recipe.description}</p>
          )}
          <div className="pending-item-meta">
            {recipe.prep_time_minutes > 0 && <span>Prep {recipe.prep_time_minutes}m</span>}
            {recipe.cook_time_minutes > 0 && <span>Cook {recipe.cook_time_minutes}m</span>}
            {recipe.servings && <span>{recipe.servings} servings</span>}
            {recipe.difficulty && <span className={`difficulty difficulty-${recipe.difficulty}`}>{recipe.difficulty}</span>}
            <span style={{ marginLeft: 'auto' }}>
              {expanded ? '▲ collapse' : '▼ expand'}
            </span>
          </div>
        </div>
        <div className="pending-item-quick" onClick={e => e.stopPropagation()}>
          <button
            className="btn btn-primary btn-sm"
            onClick={() => onApprove(recipe.id)}
            disabled={!!acting}
          >
            {acting === 'approving' ? 'Saving…' : 'Save'}
          </button>
          <button
            className="btn btn-danger btn-sm"
            onClick={() => onReject(recipe.id)}
            disabled={!!acting}
          >
            {acting === 'rejecting' ? '…' : 'Reject'}
          </button>
        </div>
      </div>

      {expanded && (
        <div className="pending-item-expanded">
          {recipe.ingredients && recipe.ingredients.length > 0 && (
            <div className="recipe-card-ingredients" style={{ marginTop: 0 }}>
              <h4>Ingredients</h4>
              <ul className="ingredients-list">
                {recipe.ingredients.map((ing, i) => (
                  <li key={i}>
                    <strong>{ing.amount} {ing.unit}</strong> {ing.name}
                    {ing.notes && <span className="ingredient-notes"> ({ing.notes})</span>}
                  </li>
                ))}
              </ul>
            </div>
          )}
          {recipe.instructions && recipe.instructions.length > 0 && (
            <div className="recipe-card-instructions">
              <h4>Instructions</h4>
              <ol className="instructions-list">
                {recipe.instructions.map((step, i) => <li key={i}>{step}</li>)}
              </ol>
            </div>
          )}
          {recipe.tags && recipe.tags.length > 0 && (
            <div className="recipe-tags" style={{ marginTop: 12 }}>
              {recipe.tags.map(tag => <span key={tag} className="tag">{tag}</span>)}
            </div>
          )}
          <p className="pending-item-date">
            Generated {new Date(recipe.created_at).toLocaleDateString(undefined, { dateStyle: 'medium' })}
          </p>
          <div className="pending-item-actions">
            <button className="btn btn-secondary" onClick={() => onEdit(recipe.id)}>
              Edit ingredients & instructions
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

export default function PendingPage({ onCountChange }) {
  const [recipes, setRecipes] = useState([]);
  const [loading, setLoading] = useState(true);
  const [bgEnabled, setBgEnabled] = useState(false);
  const [bgGenStatus, setBgGenStatus] = useState(null);
  const [acting, setActing] = useState({});
  const [editingId, setEditingId] = useState(null);
  const [editSaving, setEditSaving] = useState(false);
  const [generatingMsgState, setGeneratingMsgState] = useState(null);
  const generatingTimerRef = useRef(null);

  useEffect(() => {
    Promise.all([listPendingRecipes(), getSettings(), getFeatureStatus()]).then(([data, settings, features]) => {
      const list = data || [];
      setRecipes(list);
      onCountChange?.(list.length);
      const map = {};
      (settings || []).forEach(s => { map[s.key] = s.value; });
      setBgEnabled(map.background_generation_enabled === 'true');
      setBgGenStatus((features || {})['background-generation'] || 'unconfigured');
    }).finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    const es = new EventSource('/api/pending/events');

    es.onmessage = (e) => {
      let ev;
      try { ev = JSON.parse(e.data); } catch { return; }

      if (ev.type === 'pending_added') {
        const recipe = ev.data;
        setRecipes(rs => {
          if (rs.some(r => r.id === recipe.id)) return rs;
          const next = [recipe, ...rs];
          onCountChange?.(next.length);
          return next;
        });
        setGeneratingMsgState(null);
        clearTimeout(generatingTimerRef.current);
      } else if (ev.type === 'status' || ev.type === 'tool') {
        setGeneratingMsgState(ev.message || 'Generating recipe…');
        clearTimeout(generatingTimerRef.current);
        generatingTimerRef.current = setTimeout(() => setGeneratingMsgState(null), 60_000);
      }
    };

    return () => {
      es.close();
      clearTimeout(generatingTimerRef.current);
    };
  }, []);

  const remove = (id) => {
    setRecipes(rs => {
      const next = rs.filter(r => r.id !== id);
      onCountChange?.(next.length);
      return next;
    });
  };

  const handleApprove = async (id) => {
    setActing(a => ({ ...a, [id]: 'approving' }));
    try {
      await approvePendingRecipe(id);
      remove(id);
    } catch (err) {
      alert('Failed to save: ' + err.message);
    } finally {
      setActing(a => ({ ...a, [id]: null }));
    }
  };

  const handleReject = async (id) => {
    setActing(a => ({ ...a, [id]: 'rejecting' }));
    try {
      await rejectPendingRecipe(id);
      remove(id);
    } catch (err) {
      alert('Failed to reject: ' + err.message);
    } finally {
      setActing(a => ({ ...a, [id]: null }));
    }
  };

  const handleEditSave = async (id, { ingredients, instructions }) => {
    setEditSaving(true);
    try {
      await updatePendingRecipeContent(id, ingredients, instructions);
      setRecipes(rs => rs.map(r => r.id === id ? { ...r, ingredients, instructions } : r));
      setEditingId(null);
    } catch (err) {
      alert('Failed to save: ' + err.message);
    } finally {
      setEditSaving(false);
    }
  };

  if (loading) return <p className="empty-state">Loading...</p>;

  return (
    <div className="pending-page">
      <h2>Pending</h2>
      <p className="pending-page-hint">
        Recipes generated in the background, waiting for you. Save the ones you like or reject the rest.
        Discarded automatically after 7 days.
      </p>
      {generatingMsgState && (
        <div className="pending-generating-banner">
          <span className="pending-generating-spinner" />
          {generatingMsgState}
        </div>
      )}
      {bgEnabled && bgGenStatus && bgGenStatus !== 'available' && (
        <div className="settings-warning">
          {bgGenStatus === 'offline'
            ? 'Background generation is enabled but the provider tagged background-generation is currently offline. No new recipes will be generated until it comes back.'
            : 'Background generation is enabled but no provider is tagged background-generation. Assign the tag in Settings → LLM Providers.'}
        </div>
      )}

      {recipes.length === 0 ? (
        <p className="empty-state">
          {bgEnabled
            ? 'No pending recipes yet. More will appear here automatically.'
            : 'No pending recipes. Enable background generation in Settings to get some.'}
        </p>
      ) : (
        <div className="pending-list">
          {recipes.map(recipe => (
            <PendingItem
              key={recipe.id}
              recipe={recipe}
              acting={acting[recipe.id]}
              editing={editingId === recipe.id}
              editSaving={editSaving}
              onApprove={handleApprove}
              onReject={handleReject}
              onEdit={(id) => setEditingId(id)}
              onEditSave={({ ingredients, instructions }) => handleEditSave(recipe.id, { ingredients, instructions })}
              onEditCancel={() => setEditingId(null)}
            />
          ))}
        </div>
      )}
    </div>
  );
}
