import { useState, useEffect } from 'react';
import { getSettings } from '../api/client';

const CUISINES = ['', 'American', 'Argentine', 'Brazilian', 'British', 'Caribbean', 'Chinese', 'Colombian', 'Dutch', 'Eastern European', 'Ethiopian', 'Filipino', 'French', 'German', 'Indian', 'Italian', 'Jamaican', 'Japanese', 'Korean', 'Mediterranean', 'Mexican', 'Middle Eastern', 'Moroccan', 'Peruvian', 'Scandinavian', 'Spanish', 'Surinamese', 'Thai', 'Turkish', 'Vietnamese'];
const DIFFICULTIES = ['', 'easy', 'medium', 'hard'];
const DIETARY = ['vegetarian', 'vegan', 'gluten-free', 'dairy-free', 'keto', 'paleo'];

export default function GenerateForm({ onGenerate, loading, initialNotes = '' }) {
  const [mode, setMode] = useState('single');
  const [form, setForm] = useState({
    cuisine_type: '',
    dietary_restrictions: [],
    max_prep_time: 0,
    difficulty: '',
    servings: 4,
    additional_notes: initialNotes,
    count: 3,
  });

  useEffect(() => {
    getSettings().then(data => {
      const s = (data || []).find(s => s.key === 'default_servings');
      if (s) setForm(prev => ({ ...prev, servings: parseInt(s.value) || 4 }));
    }).catch(() => {});
  }, []);

  useEffect(() => {
    if (initialNotes) {
      setForm(prev => ({ ...prev, additional_notes: initialNotes }));
    }
  }, [initialNotes]);

  const toggleDietary = (item) => {
    setForm(prev => ({
      ...prev,
      dietary_restrictions: prev.dietary_restrictions.includes(item)
        ? prev.dietary_restrictions.filter(d => d !== item)
        : [...prev.dietary_restrictions, item],
    }));
  };

  const handleSubmit = (e) => {
    e.preventDefault();
    const { count, ...rest } = form;
    if (mode === 'batch') {
      onGenerate('batch', { ...rest, count });
    } else {
      onGenerate('single', rest);
    }
  };

  return (
    <form className="generate-form" onSubmit={handleSubmit}>
      <div className="mode-toggle">
        <button type="button" className={mode === 'single' ? 'active' : ''} onClick={() => setMode('single')}>Single</button>
        <button type="button" className={mode === 'batch' ? 'active' : ''} onClick={() => setMode('batch')}>Batch</button>
      </div>

      <div className="form-group">
        <label>Cuisine</label>
        <select value={form.cuisine_type} onChange={e => setForm(prev => ({ ...prev, cuisine_type: e.target.value }))}>
          {CUISINES.map(c => <option key={c} value={c}>{c || 'Any'}</option>)}
        </select>
      </div>

      <div className="form-group">
        <label>Difficulty</label>
        <select value={form.difficulty} onChange={e => setForm(prev => ({ ...prev, difficulty: e.target.value }))}>
          {DIFFICULTIES.map(d => <option key={d} value={d}>{d || 'Any'}</option>)}
        </select>
      </div>

      <div className="form-group">
        <label>Servings</label>
        <input type="number" min="1" max="20" value={form.servings}
          onChange={e => setForm(prev => ({ ...prev, servings: parseInt(e.target.value) || 4 }))} />
      </div>

      <div className="form-group">
        <label>Max Prep Time (minutes, 0 = any)</label>
        <input type="number" min="0" value={form.max_prep_time}
          onChange={e => setForm(prev => ({ ...prev, max_prep_time: parseInt(e.target.value) || 0 }))} />
      </div>

      <div className="form-group">
        <label>Dietary Restrictions</label>
        <div className="checkbox-group">
          {DIETARY.map(d => (
            <label key={d} className="checkbox-label">
              <input type="checkbox" checked={form.dietary_restrictions.includes(d)} onChange={() => toggleDietary(d)} />
              {d}
            </label>
          ))}
        </div>
      </div>

      <div className="form-group">
        <label>Additional Notes</label>
        <textarea value={form.additional_notes} rows={3}
          onChange={e => setForm(prev => ({ ...prev, additional_notes: e.target.value }))}
          placeholder="e.g., use seasonal ingredients, kid-friendly, quick weeknight meal..." />
      </div>

      {mode === 'batch' && (
        <div className="form-group">
          <label>Number of Recipes</label>
          <input type="number" min="2" max="10" value={form.count}
            onChange={e => setForm(prev => ({ ...prev, count: parseInt(e.target.value) || 3 }))} />
        </div>
      )}

      <button type="submit" className="btn btn-primary" disabled={loading}>
        {loading ? 'Generating...' : `Generate ${mode === 'batch' ? form.count + ' Recipes' : 'Recipe'}`}
      </button>
    </form>
  );
}
