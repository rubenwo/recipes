import { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { getRecipeHistory } from '../api/client';
import { StarRatingReadOnly } from './StarRating';

const formatDate = (iso) => {
  if (!iso) return null;
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
};

// History tab body for a saved recipe: every plan it has been on, newest cook first.
export default function RecipeHistory({ recipeId }) {
  const [entries, setEntries] = useState(null);

  useEffect(() => {
    if (!recipeId) return;
    let cancelled = false;
    getRecipeHistory(recipeId)
      .then(data => { if (!cancelled) setEntries(data || []); })
      .catch(() => { if (!cancelled) setEntries([]); });
    return () => { cancelled = true; };
  }, [recipeId]);

  if (entries == null) return <p className="empty-state">Loading history...</p>;

  const cooked = entries.filter(e => e.completed);
  const rated = cooked.filter(e => e.rating != null);
  const avgRating = rated.length > 0
    ? rated.reduce((s, e) => s + e.rating, 0) / rated.length
    : null;

  if (entries.length === 0) {
    return <p className="empty-state">This recipe hasn&apos;t been on a meal plan yet.</p>;
  }

  return (
    <div className="recipe-history">
      <div className="recipe-history-summary">
        <span><strong>{cooked.length}</strong> cook{cooked.length === 1 ? '' : 's'}</span>
        {avgRating != null && (
          <span> · avg rating <strong>{avgRating.toFixed(1)}/10</strong> ({rated.length} rated)</span>
        )}
        {cooked.length === 0 && entries.length > 0 && (
          <span> · on {entries.length} plan{entries.length === 1 ? '' : 's'} but never marked done</span>
        )}
      </div>
      <ul className="recipe-history-list">
        {entries.map((e, i) => (
          <li key={`${e.plan_id}-${i}`} className={`recipe-history-item${e.completed ? '' : ' recipe-history-item-skipped'}`}>
            <div className="recipe-history-item-main">
              <Link to={`/plans/${e.plan_id}`} className="recipe-history-plan">
                {e.plan_name}
              </Link>
              <span className={`plan-status plan-status-${e.plan_status}`}>
                {e.plan_status === 'draft' ? 'Draft' : e.plan_status === 'active' ? 'Active' : 'Done'}
              </span>
            </div>
            <div className="recipe-history-item-meta">
              {e.completed
                ? <span className="recipe-history-status recipe-history-status-done">✓ cooked{e.completed_at && ` · ${formatDate(e.completed_at)}`}</span>
                : <span className="recipe-history-status recipe-history-status-skipped">not cooked</span>}
              {e.completed && (
                <div className="recipe-history-rating">
                  <StarRatingReadOnly value={e.rating ?? null} />
                </div>
              )}
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}
