import { useState, useEffect, useRef } from 'react';
import { listPlans, createPlan, addRecipeToPlan } from '../api/client';

export default function AddToPlanMenu({ recipeId, anchorEl, onClose }) {
  const menuRef = useRef(null);
  const [plans, setPlans] = useState([]);
  const [loading, setLoading] = useState(true);
  const [adding, setAdding] = useState(null);
  const [done, setDone] = useState(null);
  const [showNewPlan, setShowNewPlan] = useState(false);
  const [newPlanName, setNewPlanName] = useState('');
  const [creating, setCreating] = useState(false);

  // Position the menu near the anchor button
  useEffect(() => {
    if (!anchorEl || !menuRef.current) return;
    const rect = anchorEl.getBoundingClientRect();
    const menu = menuRef.current;
    const menuWidth = 240;
    let left = rect.left;
    let top = rect.bottom + 6;

    // Keep within viewport
    if (left + menuWidth > window.innerWidth - 8) {
      left = Math.max(8, rect.right - menuWidth);
    }
    if (top + 300 > window.innerHeight) {
      top = Math.max(8, rect.top - 300 - 6);
    }

    menu.style.left = `${left}px`;
    menu.style.top = `${top}px`;
  }, [anchorEl]);

  useEffect(() => {
    listPlans()
      .then(data => setPlans((data || []).filter(p => p.status === 'draft')))
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const handleAddTo = async (planId) => {
    setAdding(planId);
    try {
      await addRecipeToPlan(planId, recipeId, 4);
      setDone(planId);
      setTimeout(onClose, 900);
    } catch (err) {
      alert('Could not add to plan: ' + err.message);
      setAdding(null);
    }
  };

  const handleCreate = async (e) => {
    e.preventDefault();
    if (!newPlanName.trim()) return;
    setCreating(true);
    try {
      const plan = await createPlan(newPlanName.trim());
      await addRecipeToPlan(plan.id, recipeId, 4);
      setDone('new');
      setTimeout(onClose, 900);
    } catch (err) {
      alert('Could not create plan: ' + err.message);
      setCreating(false);
    }
  };

  return (
    <>
      <div className="add-to-plan-backdrop" onClick={onClose} />
      <div className="add-to-plan-menu" ref={menuRef}>
        <div className="add-to-plan-header">Add to plan</div>

        {loading ? (
          <div className="add-to-plan-empty">Loading...</div>
        ) : (
          <>
            {plans.length === 0 && !showNewPlan && (
              <div className="add-to-plan-empty">No draft plans yet</div>
            )}
            {plans.map(plan => (
              <button
                key={plan.id}
                className={`add-to-plan-item${done === plan.id ? ' add-to-plan-item-done' : ''}`}
                onClick={() => handleAddTo(plan.id)}
                disabled={adding !== null || done !== null}
              >
                <span>{plan.name}</span>
                {adding === plan.id && <span className="add-to-plan-spinner" />}
                {done === plan.id && <span className="add-to-plan-check">&#10003;</span>}
              </button>
            ))}

            {showNewPlan ? (
              <form className="add-to-plan-form" onSubmit={handleCreate}>
                <input
                  autoFocus
                  type="text"
                  placeholder="Plan name…"
                  value={newPlanName}
                  onChange={e => setNewPlanName(e.target.value)}
                />
                <button type="submit" disabled={!newPlanName.trim() || creating}>
                  {creating ? '…' : 'Create & add'}
                </button>
              </form>
            ) : (
              <button className="add-to-plan-new" onClick={() => setShowNewPlan(true)}>
                + New plan
              </button>
            )}
          </>
        )}
      </div>
    </>
  );
}
