import { useState, useEffect } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { saveRecipe } from '../api/client';
import { CUISINES, DIFFICULTIES, DIETARY, SUGGESTED_TAGS } from '../constants/recipe';
import GenerateForm from '../components/GenerateForm';
import GenerationProgress from '../components/GenerationProgress';
import ReviewPanel from '../components/ReviewPanel';
import RecipeEditForm from '../components/RecipeEditForm';
import { useGeneration } from '../hooks/useGeneration';

const TABS = ['generate', 'import', 'manual'];

const EMPTY_RECIPE = { ingredients: [], instructions: [] };

function ManualTab() {
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
    if (!header.title.trim()) { setError('Title is required.'); return; }
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
    <div className="recipe-detail">
      {error && <div className="error-message" style={{ margin: '16px 28px 0' }}>{error}</div>}

      <div className="recipe-header-fields">
        <div className="form-group">
          <label>Title *</label>
          <input type="text" className="edit-input" value={header.title}
            onChange={e => set('title', e.target.value)} placeholder="Recipe title" />
        </div>

        <div className="form-group">
          <label>Description</label>
          <textarea className="edit-input" rows={2} value={header.description}
            onChange={e => set('description', e.target.value)} placeholder="Short description" />
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
            <input type="number" min="1" max="100" className="edit-input" value={header.servings}
              onChange={e => set('servings', e.target.value)} />
          </div>
        </div>

        <div className="recipe-meta-row">
          <div className="form-group">
            <label>Prep time (min)</label>
            <input type="number" min="0" className="edit-input" value={header.prep_time_minutes}
              onChange={e => set('prep_time_minutes', e.target.value)} placeholder="0" />
          </div>
          <div className="form-group">
            <label>Cook time (min)</label>
            <input type="number" min="0" className="edit-input" value={header.cook_time_minutes}
              onChange={e => set('cook_time_minutes', e.target.value)} placeholder="0" />
          </div>
        </div>

        <div className="form-group">
          <label>Dietary</label>
          <div className="checkbox-group">
            {DIETARY.map(d => (
              <label key={d} className="checkbox-label">
                <input type="checkbox" checked={header.dietary_restrictions.includes(d)} onChange={() => toggleDietary(d)} />
                {d}
              </label>
            ))}
          </div>
        </div>

        <div className="form-group">
          <label>Tags</label>
          <div className="checkbox-group" style={{ marginBottom: '8px' }}>
            {SUGGESTED_TAGS.map(tag => {
              const active = header.tags.split(',').map(t => t.trim()).includes(tag);
              return (
                <label key={tag} className="checkbox-label">
                  <input type="checkbox" checked={active} onChange={() => toggleTag(tag)} />
                  {tag}
                </label>
              );
            })}
          </div>
          <input type="text" className="edit-input" value={header.tags}
            onChange={e => set('tags', e.target.value)}
            placeholder="or type custom tags, comma-separated" />
        </div>
      </div>

      <RecipeEditForm recipe={EMPTY_RECIPE} onSave={handleSave}
        onCancel={() => navigate('/library')} saving={saving} />
    </div>
  );
}

function ImportTab() {
  const { events, recipes, loading, error, generate, reset, removeRecipe } = useGeneration();
  const [rawText, setRawText] = useState('');

  const handleImport = (e) => {
    e.preventDefault();
    if (!rawText.trim()) return;
    reset();
    generate('import', { raw_text: rawText });
  };

  const handleRefine = (recipe, feedback) => {
    generate('refine', { recipe, feedback });
  };

  const progressEvents = events.filter(e => e.type !== 'recipe');

  return (
    <div>
      <form onSubmit={handleImport} className="import-form">
        <textarea
          value={rawText}
          onChange={e => setRawText(e.target.value)}
          placeholder="Paste your recipe here — just a name is enough, but you can include ingredients and instructions too"
          rows={8}
          disabled={loading}
        />
        <button className="btn btn-primary" type="submit" disabled={loading || !rawText.trim()}>
          {loading ? 'Importing...' : 'Import Recipe'}
        </button>
      </form>

      {error && <div className="error-message">{error}</div>}
      <GenerationProgress events={progressEvents} loading={loading} hasRecipes={recipes.length > 0} />
      <ReviewPanel recipes={recipes} onRefine={handleRefine} onRemove={removeRecipe} loading={loading} />
    </div>
  );
}

function GenerateTab({ initialNotes }) {
  const { events, recipes, loading, error, generate, reset, removeRecipe } = useGeneration();

  const handleGenerate = (endpoint, body) => {
    reset();
    generate(endpoint, body);
  };

  const handleRefine = (recipe, feedback) => {
    generate('refine', { recipe, feedback });
  };

  const progressEvents = events.filter(e => e.type !== 'recipe');

  return (
    <div className="generate-layout">
      <div className="generate-left">
        <GenerateForm onGenerate={handleGenerate} loading={loading} initialNotes={initialNotes} />
      </div>
      <div className="generate-right">
        {error && <div className="error-message">{error}</div>}
        <GenerationProgress events={progressEvents} loading={loading} hasRecipes={recipes.length > 0} />
        <ReviewPanel recipes={recipes} onRefine={handleRefine} onRemove={removeRecipe} loading={loading} />
      </div>
    </div>
  );
}

export default function AddRecipePage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const initialTab = TABS.includes(searchParams.get('mode')) ? searchParams.get('mode') : 'generate';
  const [tab, setTab] = useState(initialTab);
  const initialNotes = searchParams.get('notes') || '';

  useEffect(() => {
    setSearchParams(p => { p.set('mode', tab); return p; }, { replace: true });
  }, [tab]);

  return (
    <div className="generate-page">
      <div className="add-recipe-tabs">
        {TABS.map(t => (
          <button key={t} type="button"
            className={`add-recipe-tab${tab === t ? ' active' : ''}`}
            onClick={() => setTab(t)}>
            {t.charAt(0).toUpperCase() + t.slice(1)}
          </button>
        ))}
      </div>

      {tab === 'generate' && <GenerateTab initialNotes={initialNotes} />}
      {tab === 'import' && <ImportTab />}
      {tab === 'manual' && <ManualTab />}
    </div>
  );
}
