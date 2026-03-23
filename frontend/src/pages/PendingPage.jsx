import { useState, useEffect, useRef } from 'react';
import { listPendingRecipes, approvePendingRecipe, rejectPendingRecipe, getSettings, updatePendingRecipeContent, getFeatureStatus } from '../api/client';
import RecipeCard from '../components/RecipeCard';
import RecipeEditForm from '../components/RecipeEditForm';

export default function PendingPage({ onCountChange }) {
  const [recipes, setRecipes] = useState([]);
  const [loading, setLoading] = useState(true);
  const [bgEnabled, setBgEnabled] = useState(false);
  const [bgGenStatus, setBgGenStatus] = useState(null);
  const [acting, setActing] = useState({});
  const [editingId, setEditingId] = useState(null);
  const [editSaving, setEditSaving] = useState(false);
  const [revisions, setRevisions] = useState({});
  const generatingMsg = useRef(null);
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

  // Stream background generation events from the server.
  useEffect(() => {
    const es = new EventSource('/api/pending/events');

    es.onmessage = (e) => {
      let ev;
      try { ev = JSON.parse(e.data); } catch { return; }

      if (ev.type === 'pending_added') {
        const recipe = ev.data;
        setRecipes(rs => {
          // Avoid duplicates if the page somehow receives the same event twice.
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
        // Clear the banner if nothing new arrives within 60 seconds.
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
      setRevisions(prev => ({ ...prev, [id]: (prev[id] || 0) + 1 }));
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
      <h2>Pending Recipes</h2>
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
      <p className="pending-page-hint">
        These recipes were generated in the background. Save the ones you like to your library, or reject the rest.
        Pending recipes are automatically discarded after 7 days.
      </p>

      {recipes.length === 0 ? (
        <p className="empty-state">
          {bgEnabled
            ? 'No pending recipes yet. More will appear here automatically.'
            : 'No pending recipes. Enable background generation in Settings to get some.'}
        </p>
      ) : (
        <div className="pending-list">
          {recipes.map(recipe => (
            <div key={recipe.id} className="pending-item">
              {editingId === recipe.id ? (
                <RecipeEditForm
                  recipe={recipe}
                  onSave={({ ingredients, instructions }) => handleEditSave(recipe.id, { ingredients, instructions })}
                  onCancel={() => setEditingId(null)}
                  saving={editSaving}
                />
              ) : (
                <>
                  <RecipeCard
                    key={revisions[recipe.id] || 0}
                    recipe={recipe}
                    showIngredients
                    fetchImageEndpoint={`/pending/${recipe.id}/fetch-image`}
                  />
                  <p className="pending-item-date">
                    Generated {new Date(recipe.created_at).toLocaleDateString(undefined, { dateStyle: 'medium' })}
                  </p>
                  <div className="pending-item-actions">
                    <button
                      className="btn btn-primary"
                      onClick={() => handleApprove(recipe.id)}
                      disabled={!!acting[recipe.id]}
                    >
                      {acting[recipe.id] === 'approving' ? 'Saving…' : 'Save to library'}
                    </button>
                    <button
                      className="btn btn-secondary"
                      onClick={() => setEditingId(recipe.id)}
                      disabled={!!acting[recipe.id]}
                    >
                      Edit
                    </button>
                    <button
                      className="btn btn-danger"
                      onClick={() => handleReject(recipe.id)}
                      disabled={!!acting[recipe.id]}
                    >
                      {acting[recipe.id] === 'rejecting' ? 'Rejecting…' : 'Reject'}
                    </button>
                  </div>
                </>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
