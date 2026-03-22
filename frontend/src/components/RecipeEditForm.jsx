import { useState } from 'react';

export default function RecipeEditForm({ recipe, onSave, onCancel, saving }) {
  const [ingredients, setIngredients] = useState(
    (recipe.ingredients || []).map(ing => ({
      ...ing,
      amount: ing.amount === 0 ? '' : String(ing.amount),
    }))
  );
  const [instructions, setInstructions] = useState([...(recipe.instructions || [])]);

  const updateIng = (i, field, value) =>
    setIngredients(prev => prev.map((ing, idx) => idx === i ? { ...ing, [field]: value } : ing));

  const removeIng = (i) => setIngredients(prev => prev.filter((_, idx) => idx !== i));
  const addIng = () => setIngredients(prev => [...prev, { amount: '', unit: '', name: '', notes: '' }]);

  const updateStep = (i, value) =>
    setInstructions(prev => prev.map((s, idx) => idx === i ? value : s));
  const removeStep = (i) => setInstructions(prev => prev.filter((_, idx) => idx !== i));
  const addStep = () => setInstructions(prev => [...prev, '']);

  const handleSave = () => {
    const parsedIngredients = ingredients.map(ing => ({
      ...ing,
      amount: parseFloat(ing.amount) || 0,
    }));
    onSave({ ingredients: parsedIngredients, instructions });
  };

  return (
    <div className="recipe-edit-form">
      <section className="recipe-edit-section">
        <h4>Ingredients</h4>
        <ul className="recipe-edit-list">
          {ingredients.map((ing, i) => (
            <li key={i} className="recipe-edit-ingredient-row">
              <input
                type="number"
                min="0"
                step="any"
                value={ing.amount}
                onChange={e => updateIng(i, 'amount', e.target.value)}
                placeholder="Qty"
                className="edit-input edit-input-amount"
              />
              <input
                type="text"
                value={ing.unit || ''}
                onChange={e => updateIng(i, 'unit', e.target.value)}
                placeholder="Unit"
                className="edit-input edit-input-unit"
              />
              <input
                type="text"
                value={ing.name || ''}
                onChange={e => updateIng(i, 'name', e.target.value)}
                placeholder="Ingredient"
                className="edit-input edit-input-name"
              />
              <input
                type="text"
                value={ing.notes || ''}
                onChange={e => updateIng(i, 'notes', e.target.value)}
                placeholder="Notes"
                className="edit-input edit-input-notes"
              />
              <button
                type="button"
                className="btn btn-danger btn-sm"
                onClick={() => removeIng(i)}
                title="Remove ingredient"
              >×</button>
            </li>
          ))}
        </ul>
        <button type="button" className="btn btn-secondary btn-sm" onClick={addIng}>+ Add ingredient</button>
      </section>

      <section className="recipe-edit-section">
        <h4>Instructions</h4>
        <ol className="recipe-edit-list recipe-edit-instructions-list">
          {instructions.map((step, i) => (
            <li key={i} className="recipe-edit-instruction-row">
              <textarea
                value={step}
                onChange={e => updateStep(i, e.target.value)}
                rows={2}
                className="edit-instruction-textarea"
              />
              <button
                type="button"
                className="btn btn-danger btn-sm"
                onClick={() => removeStep(i)}
                title="Remove step"
              >×</button>
            </li>
          ))}
        </ol>
        <button type="button" className="btn btn-secondary btn-sm" onClick={addStep}>+ Add step</button>
      </section>

      <div className="recipe-edit-actions">
        <button className="btn btn-primary" onClick={handleSave} disabled={saving}>
          {saving ? 'Saving…' : 'Save changes'}
        </button>
        <button className="btn btn-secondary" onClick={onCancel} disabled={saving}>Cancel</button>
      </div>
    </div>
  );
}
