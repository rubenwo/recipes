import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { saveRecipe } from '../api/client';
import RecipeEditForm from '../components/RecipeEditForm';

const CUISINES = ['', 'American', 'Argentine', 'Brazilian', 'British', 'Caribbean', 'Chinese', 'Dutch', 'Eastern European', 'Ethiopian', 'Filipino', 'French', 'German', 'Indian', 'Italian', 'Japanese', 'Korean', 'Mediterranean', 'Mexican', 'Middle Eastern', 'Moroccan', 'Peruvian', 'Scandinavian', 'Spanish', 'Thai', 'Turkish', 'Vietnamese'];
const DIFFICULTIES = ['', 'easy', 'medium', 'hard'];
const DIETARY = ['vegetarian', 'vegan', 'gluten-free', 'dairy-free', 'keto', 'paleo'];
const SUGGESTED_TAGS = ['high-protein', 'low-carb', 'omega-3', 'low-calorie', 'high-fiber', 'meal-prep', 'quick', 'budget-friendly', 'one-pot', 'freezer-friendly'];

const EMPTY_RECIPE = { ingredients: [], instructions: [] };

export default function CreateRecipePage() {
  const navigate = useNavigate();
  const [header, setHeader] = useState({
    title: '',
    description: '',
    cuisine_type: '',
    prep_time_minutes: '',
    cook_time_minutes: '',
    servings: 4,
    difficulty: '',
    dietary_restrictions: [],
    tags: '',
  });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);

  const set = (field, value) => setHeader(prev => ({ ...prev, [field]: value }));

  const toggleDietary = (item) => {
    setHeader(prev => ({
      ...prev,
      dietary_restrictions: prev.dietary_restrictions.includes(item)
        ? prev.dietary_restrictions.filter(d => d !== item)
        : [...prev.dietary_restrictions, item],
    }));
  };

  const toggleTag = (tag) => {
    setHeader(prev => {
      const current = prev.tags.split(',').map(t => t.trim()).filter(Boolean);
      const updated = current.includes(tag)
        ? current.filter(t => t !== tag)
        : [...current, tag];
      return { ...prev, tags: updated.join(', ') };
    });
  };

  const handleSave = async ({ ingredients, instructions }) => {
    if (!header.title.trim()) {
      setError('Title is required.');
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const recipe = {
        ...header,
        prep_time_minutes: parseInt(header.prep_time_minutes) || 0,
        cook_time_minutes: parseInt(header.cook_time_minutes) || 0,
        servings: parseInt(header.servings) || 4,
        tags: header.tags.split(',').map(t => t.trim()).filter(Boolean),
        ingredients,
        instructions,
      };
      const saved = await saveRecipe(recipe);
      navigate(`/recipe/${saved.id}`);
    } catch (err) {
      setError(err.message || 'Failed to save recipe.');
      setSaving(false);
    }
  };

  return (
    <div className="recipe-page">
      <div className="recipe-detail">
        <div className="recipe-create-header">
          <h2 className="recipe-create-heading">New Recipe</h2>
          <p className="recipe-create-subheading">Add a recipe to your library</p>
        </div>

        {error && <div className="error-message" style={{margin: '16px 28px 0'}}>{error}</div>}

        <div className="recipe-header-fields">
          <div className="form-group">
            <label>Title *</label>
            <input
              type="text"
              className="edit-input"
              value={header.title}
              onChange={e => set('title', e.target.value)}
              placeholder="Recipe title"
            />
          </div>

          <div className="form-group">
            <label>Description</label>
            <textarea
              className="edit-input"
              rows={2}
              value={header.description}
              onChange={e => set('description', e.target.value)}
              placeholder="Short description"
            />
          </div>

          <div className="recipe-meta-row">
            <div className="form-group">
              <label>Cuisine</label>
              <select value={header.cuisine_type} onChange={e => set('cuisine_type', e.target.value)}>
                {CUISINES.map(c => <option key={c} value={c}>{c || 'None'}</option>)}
              </select>
            </div>

            <div className="form-group">
              <label>Difficulty</label>
              <select value={header.difficulty} onChange={e => set('difficulty', e.target.value)}>
                {DIFFICULTIES.map(d => <option key={d} value={d}>{d || 'None'}</option>)}
              </select>
            </div>

            <div className="form-group">
              <label>Servings</label>
              <input
                type="number"
                min="1"
                max="100"
                className="edit-input"
                value={header.servings}
                onChange={e => set('servings', e.target.value)}
              />
            </div>
          </div>

          <div className="recipe-meta-row">
            <div className="form-group">
              <label>Prep time (min)</label>
              <input
                type="number"
                min="0"
                className="edit-input"
                value={header.prep_time_minutes}
                onChange={e => set('prep_time_minutes', e.target.value)}
                placeholder="0"
              />
            </div>

            <div className="form-group">
              <label>Cook time (min)</label>
              <input
                type="number"
                min="0"
                className="edit-input"
                value={header.cook_time_minutes}
                onChange={e => set('cook_time_minutes', e.target.value)}
                placeholder="0"
              />
            </div>
          </div>

          <div className="form-group">
            <label>Dietary</label>
            <div className="checkbox-group">
              {DIETARY.map(d => (
                <label key={d} className="checkbox-label">
                  <input
                    type="checkbox"
                    checked={header.dietary_restrictions.includes(d)}
                    onChange={() => toggleDietary(d)}
                  />
                  {d}
                </label>
              ))}
            </div>
          </div>

          <div className="form-group">
            <label>Tags</label>
            <div className="checkbox-group" style={{marginBottom: '8px'}}>
              {SUGGESTED_TAGS.map(tag => {
                const active = header.tags.split(',').map(t => t.trim()).includes(tag);
                return (
                  <label key={tag} className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={active}
                      onChange={() => toggleTag(tag)}
                    />
                    {tag}
                  </label>
                );
              })}
            </div>
            <input
              type="text"
              className="edit-input"
              value={header.tags}
              onChange={e => set('tags', e.target.value)}
              placeholder="or type custom tags, comma-separated"
            />
          </div>
        </div>

        <RecipeEditForm
          recipe={EMPTY_RECIPE}
          onSave={handleSave}
          onCancel={() => navigate('/library')}
          saving={saving}
        />
      </div>
    </div>
  );
}
