import { useState, useEffect } from 'react';
import {
  listProviders, createProvider, updateProvider, deleteProvider,
  getSettings, updateSettings, listModels, runTranslationNow,
} from '../api/client';

export default function SettingsPage() {
  return (
    <div className="settings-page">
      <h2>Settings</h2>
      <ProvidersSection />
      <GeneralSettings />
      <BackgroundGenerationSettings />
      <BackgroundTranslationSettings />
    </div>
  );
}

const KNOWN_TAGS = ['generation', 'background-generation', 'chat', 'search', 'translation'];

function TagPicker({ tags = [], onChange }) {
  const [selected, setSelected] = useState('');

  const add = () => {
    if (!selected || tags.includes(selected)) return;
    onChange([...tags, selected]);
    setSelected('');
  };

  const remove = (tag) => onChange(tags.filter(t => t !== tag));

  const availableKnown = KNOWN_TAGS.filter(t => !tags.includes(t));

  return (
    <div className="tag-picker">
      {tags.length > 0 && (
        <div className="tag-picker-chips">
          {tags.map(t => (
            <span key={t} className="tag tag-chip">
              {t}
              <button type="button" className="tag-chip-remove" onClick={() => remove(t)} aria-label={`Remove ${t}`}>
                &times;
              </button>
            </span>
          ))}
        </div>
      )}
      <div className="tag-picker-input-row">
        <select
          value={selected}
          onChange={e => setSelected(e.target.value)}
        >
          <option value="">Add tag…</option>
          {availableKnown.map(t => <option key={t} value={t}>{t}</option>)}
        </select>
        <button
          type="button"
          className="btn btn-secondary"
          onClick={add}
          disabled={!selected}
        >
          Add
        </button>
      </div>
    </div>
  );
}

function ModelInput({ host, providerType = 'ollama', value, onChange, id }) {
  const [models, setModels] = useState([]);

  useEffect(() => {
    if (!host) { setModels([]); return; }
    let cancelled = false;
    listModels(host, providerType)
      .then(data => { if (!cancelled) setModels(data || []); })
      .catch(() => { if (!cancelled) setModels([]); });
    return () => { cancelled = true; };
  }, [host, providerType]);

  return (
    <>
      <input
        list={id}
        value={value}
        onChange={onChange}
        placeholder="Model (e.g. qwen3.5:9b)"
      />
      <datalist id={id}>
        {models.map(m => <option key={m} value={m} />)}
      </datalist>
    </>
  );
}

function ProvidersSection() {
  const [providers, setProviders] = useState([]);
  const [loading, setLoading] = useState(true);
  const [form, setForm] = useState({ name: '', host: '', model: '', provider_type: 'ollama', tags: [] });
  const [editing, setEditing] = useState(null);

  useEffect(() => {
    listProviders().then(data => { setProviders(data || []); setLoading(false); });
  }, []);

  const reload = async () => {
    const data = await listProviders();
    setProviders(data || []);
  };

  const handleAdd = async (e) => {
    e.preventDefault();
    if (!form.name || !form.host || !form.model) return;
    await createProvider({ ...form, enabled: true });
    setForm({ name: '', host: '', model: '', provider_type: 'ollama', tags: [] });
    reload();
  };

  const handleDelete = async (id) => {
    await deleteProvider(id);
    reload();
  };

  const handleToggle = async (p) => {
    await updateProvider(p.id, { ...p, enabled: !p.enabled });
    reload();
  };

  const handleEditSave = async () => {
    if (!editing) return;
    await updateProvider(editing.id, editing);
    setEditing(null);
    reload();
  };

  if (loading) return <p className="empty-state">Loading...</p>;

  return (
    <div className="settings-section">
      <h3>LLM Providers</h3>
      <p className="settings-description">
        Configure LLM endpoints. Supports native Ollama and any OpenAI-compatible API (LiteLLM, vLLM, llama.cpp).
        Batch generation distributes work across enabled providers.
      </p>

      <div className="providers-list">
        {providers.map(p => (
          <div key={p.id} className={`provider-item ${!p.enabled ? 'provider-disabled' : ''}`}>
            {editing?.id === p.id ? (
              <div className="provider-edit-form">
                <input
                  value={editing.name}
                  onChange={e => setEditing({ ...editing, name: e.target.value })}
                  placeholder="Name"
                />
                <input
                  value={editing.host}
                  onChange={e => setEditing({ ...editing, host: e.target.value })}
                  placeholder="Host URL"
                />
                <select
                  value={editing.provider_type || 'ollama'}
                  onChange={e => setEditing({ ...editing, provider_type: e.target.value })}
                >
                  <option value="ollama">Ollama</option>
                  <option value="openai_compat">OpenAI-compatible</option>
                </select>
                <ModelInput
                  host={editing.host}
                  providerType={editing.provider_type || 'ollama'}
                  value={editing.model}
                  onChange={e => setEditing({ ...editing, model: e.target.value })}
                  id={`edit-models-${editing.id}`}
                />
                <TagPicker
                  tags={editing.tags || []}
                  onChange={tags => setEditing({ ...editing, tags })}
                />
                <div className="provider-edit-actions">
                  <button className="btn btn-primary" onClick={handleEditSave}>Save</button>
                  <button className="btn btn-secondary" onClick={() => setEditing(null)}>Cancel</button>
                </div>
              </div>
            ) : (
              <>
                <div className="provider-info">
                  <strong>{p.name}</strong>
                  <span className="provider-host">{p.host}</span>
                  <span className="provider-model">{p.model}</span>
                  {p.provider_type && p.provider_type !== 'ollama' && (
                    <span className="tag">{p.provider_type}</span>
                  )}
                  {p.tags && p.tags.length > 0 && (
                    <span className="provider-tags">
                      {p.tags.map(t => <span key={t} className="tag">{t}</span>)}
                    </span>
                  )}
                  <span className={`health-badge health-${p.health_status}`}>{p.health_status}</span>
                </div>
                <div className="provider-actions">
                  <button
                    className={`btn ${p.enabled ? 'btn-secondary' : 'btn-primary'}`}
                    onClick={() => handleToggle(p)}
                  >
                    {p.enabled ? 'Disable' : 'Enable'}
                  </button>
                  <button className="btn btn-secondary" onClick={() => setEditing({ ...p })}>Edit</button>
                  <button className="btn btn-danger" onClick={() => handleDelete(p.id)}>Delete</button>
                </div>
              </>
            )}
          </div>
        ))}
        {providers.length === 0 && (
          <p className="empty-state">No providers configured.</p>
        )}
      </div>

      <form onSubmit={handleAdd} className="provider-add-form">
        <h4>Add Provider</h4>
        <div className="provider-add-fields">
          <input
            value={form.name}
            onChange={e => setForm({ ...form, name: e.target.value })}
            placeholder="Name (e.g. GPU Server)"
          />
          <input
            value={form.host}
            onChange={e => setForm({ ...form, host: e.target.value })}
            placeholder="Host URL (e.g. http://192.168.1.100:11434)"
          />
          <select
            value={form.provider_type}
            onChange={e => setForm({ ...form, provider_type: e.target.value })}
          >
            <option value="ollama">Ollama</option>
            <option value="openai_compat">OpenAI-compatible</option>
          </select>
          <ModelInput
            host={form.host}
            providerType={form.provider_type}
            value={form.model}
            onChange={e => setForm({ ...form, model: e.target.value })}
            id="add-models"
          />
          <TagPicker
            tags={form.tags}
            onChange={tags => setForm(f => ({ ...f, tags }))}
          />
        </div>
        <button
          className="btn btn-primary"
          type="submit"
          disabled={!form.name || !form.host || !form.model}
        >
          Add Provider
        </button>
      </form>
    </div>
  );
}

// Weekday order for display: Mon–Sun. Values match Go's time.Weekday (0=Sunday).
const WEEKDAYS = [
  { label: 'Mon', value: '1' },
  { label: 'Tue', value: '2' },
  { label: 'Wed', value: '3' },
  { label: 'Thu', value: '4' },
  { label: 'Fri', value: '5' },
  { label: 'Sat', value: '6' },
  { label: 'Sun', value: '0' },
];

function parseDays(val) {
  if (!val) return new Set();
  return new Set(val.split(',').map(d => d.trim()).filter(Boolean));
}

function serializeDays(set) {
  return [...set].join(',');
}

function BackgroundGenerationSettings() {
  const [settings, setSettings] = useState({
    background_generation_enabled: 'false',
    background_generation_days: '1,2,3,4,5',
    background_generation_time: '08:00',
    background_generation_count: '1',
    background_generation_max_retries: '3',
  });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    getSettings().then(data => {
      const map = {};
      (data || []).forEach(s => { map[s.key] = s.value; });
      setSettings(prev => ({ ...prev, ...map }));
      setLoading(false);
    });
  }, []);

  const handleSave = async () => {
    setSaving(true);
    try {
      await updateSettings({
        background_generation_enabled: settings.background_generation_enabled,
        background_generation_days: settings.background_generation_days,
        background_generation_time: settings.background_generation_time,
        background_generation_count: settings.background_generation_count,
        background_generation_max_retries: settings.background_generation_max_retries,
      });
    } catch (err) {
      alert('Failed to save: ' + err.message);
    } finally {
      setSaving(false);
    }
  };

  if (loading) return null;

  const enabled = settings.background_generation_enabled === 'true';
  const selectedDays = parseDays(settings.background_generation_days);

  const toggleDay = (value) => {
    const next = new Set(selectedDays);
    if (next.has(value)) next.delete(value);
    else next.add(value);
    setSettings(s => ({ ...s, background_generation_days: serializeDays(next) }));
  };

  return (
    <div className="settings-section">
      <h3>Background Generation</h3>
      <p className="settings-description">
        Automatically generate recipes on a cron-like schedule. Tag a provider with <code>background-generation</code>
        (e.g. your always-on server) to dedicate it to this task.
      </p>
      <div className="settings-form">
        <div className="settings-field">
          <label>
            <input
              type="checkbox"
              checked={enabled}
              onChange={e => setSettings(s => ({ ...s, background_generation_enabled: e.target.checked ? 'true' : 'false' }))}
              style={{ marginRight: 8 }}
            />
            Enable background generation
          </label>
        </div>
        {enabled && (
          <>
            <div className="settings-field">
              <label>Days</label>
              <div className="day-picker">
                {WEEKDAYS.map(d => (
                  <label key={d.value} className={`day-chip ${selectedDays.has(d.value) ? 'day-chip-active' : ''}`}>
                    <input
                      type="checkbox"
                      checked={selectedDays.has(d.value)}
                      onChange={() => toggleDay(d.value)}
                      style={{ display: 'none' }}
                    />
                    {d.label}
                  </label>
                ))}
              </div>
              <span className="settings-hint">Days of the week to run generation</span>
            </div>
            <div className="settings-field">
              <label>Time of day</label>
              <input
                type="time"
                value={settings.background_generation_time}
                onChange={e => setSettings(s => ({ ...s, background_generation_time: e.target.value }))}
                required
              />
              <span className="settings-hint">Time to run generation on selected days</span>
            </div>
            <div className="settings-field">
              <label>Recipes per run</label>
              <input
                type="number"
                min="1"
                max="10"
                value={settings.background_generation_count}
                onChange={e => setSettings(s => ({ ...s, background_generation_count: e.target.value }))}
              />
              <span className="settings-hint">Number of recipes generated each run</span>
            </div>
            <div className="settings-field">
              <label>Max retries on invalid JSON</label>
              <input
                type="number"
                min="0"
                max="10"
                value={settings.background_generation_max_retries}
                onChange={e => setSettings(s => ({ ...s, background_generation_max_retries: e.target.value }))}
              />
              <span className="settings-hint">How many times to retry a recipe if the model returns invalid JSON (0 = no retries)</span>
            </div>
          </>
        )}
        <button className="btn btn-primary" onClick={handleSave} disabled={saving}>
          {saving ? 'Saving...' : 'Save'}
        </button>
      </div>
    </div>
  );
}

function BackgroundTranslationSettings() {
  const [providers, setProviders] = useState([]);
  const [settings, setSettings] = useState({
    background_translation_enabled: 'false',
    background_translation_days: '1,2,3,4,5',
    background_translation_time: '03:00',
  });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [running, setRunning] = useState(false);
  const [runResult, setRunResult] = useState(null);

  useEffect(() => {
    Promise.all([listProviders(), getSettings()]).then(([provs, data]) => {
      setProviders(provs || []);
      const map = {};
      (data || []).forEach(s => { map[s.key] = s.value; });
      setSettings(prev => ({ ...prev, ...map }));
      setLoading(false);
    });
  }, []);

  const handleRunNow = async () => {
    setRunning(true);
    setRunResult(null);
    try {
      const res = await runTranslationNow();
      setRunResult(res.message || `Translated ${res.translated} ingredient(s).`);
    } catch (err) {
      setRunResult('Error: ' + err.message);
    } finally {
      setRunning(false);
    }
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await updateSettings({
        background_translation_enabled: settings.background_translation_enabled,
        background_translation_days: settings.background_translation_days,
        background_translation_time: settings.background_translation_time,
      });
    } catch (err) {
      alert('Failed to save: ' + err.message);
    } finally {
      setSaving(false);
    }
  };

  if (loading) return null;

  const hasTranslationProvider = providers.some(p => p.enabled && p.tags && p.tags.includes('translation'));
  const enabled = settings.background_translation_enabled === 'true';
  const selectedDays = parseDays(settings.background_translation_days);

  const toggleDay = (value) => {
    const next = new Set(selectedDays);
    if (next.has(value)) next.delete(value);
    else next.add(value);
    setSettings(s => ({ ...s, background_translation_days: serializeDays(next) }));
  };

  return (
    <div className="settings-section">
      <h3>Background Translation</h3>
      <p className="settings-description">
        Pre-populate the translation cache on a schedule so ingredients are already translated
        when you open Albert Heijn ordering. Tag an LLM provider with <code>translation</code> to
        use a dedicated (lighter) model.
      </p>

      {!hasTranslationProvider && (
        <div className="settings-warning">
          No enabled provider with the <strong>translation</strong> tag found. Background
          translation requires at least one enabled provider tagged <code>translation</code>.
          Add the tag in the LLM Providers section above.
        </div>
      )}

      <div className="settings-form">
        <div className="settings-field">
          <label>
            <input
              type="checkbox"
              checked={enabled}
              onChange={e => setSettings(s => ({ ...s, background_translation_enabled: e.target.checked ? 'true' : 'false' }))}
              style={{ marginRight: 8 }}
            />
            Enable background translation
          </label>
        </div>
        {enabled && (
          <>
            <div className="settings-field">
              <label>Days</label>
              <div className="day-picker">
                {WEEKDAYS.map(d => (
                  <label key={d.value} className={`day-chip ${selectedDays.has(d.value) ? 'day-chip-active' : ''}`}>
                    <input
                      type="checkbox"
                      checked={selectedDays.has(d.value)}
                      onChange={() => toggleDay(d.value)}
                      style={{ display: 'none' }}
                    />
                    {d.label}
                  </label>
                ))}
              </div>
              <span className="settings-hint">Days of the week to run translation</span>
            </div>
            <div className="settings-field">
              <label>Time of day</label>
              <input
                type="time"
                value={settings.background_translation_time}
                onChange={e => setSettings(s => ({ ...s, background_translation_time: e.target.value }))}
                required
              />
              <span className="settings-hint">Time to run the translation job (up to {50} ingredients per run)</span>
            </div>
          </>
        )}
        <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
          <button className="btn btn-primary" onClick={handleSave} disabled={saving}>
            {saving ? 'Saving...' : 'Save'}
          </button>
          <button
            className="btn btn-secondary"
            onClick={handleRunNow}
            disabled={running || !hasTranslationProvider}
            title={!hasTranslationProvider ? 'No translation provider configured' : 'Run translation job immediately'}
          >
            {running ? 'Running…' : 'Run Now'}
          </button>
          {runResult && <span className="settings-hint">{runResult}</span>}
        </div>
      </div>
    </div>
  );
}

function GeneralSettings() {
  const [settings, setSettings] = useState({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    getSettings().then(data => {
      const map = {};
      (data || []).forEach(s => { map[s.key] = s.value; });
      setSettings(map);
      setLoading(false);
    });
  }, []);

  const handleChange = (key, value) => {
    setSettings(prev => ({ ...prev, [key]: value }));
  };

  const GENERAL_KEYS = ['suggestion_count', 'default_servings', 'max_tool_iterations', 'generation_timeout', 'ui_language'];

  const handleSave = async () => {
    setSaving(true);
    try {
      const payload = Object.fromEntries(GENERAL_KEYS.filter(k => k in settings).map(k => [k, settings[k]]));
      await updateSettings(payload);
    } catch (err) {
      alert('Failed to save settings: ' + err.message);
    } finally {
      setSaving(false);
    }
  };

  if (loading) return null;

  return (
    <div className="settings-section">
      <h3>General</h3>
      <div className="settings-form">
        <div className="settings-field">
          <label>Daily suggestions</label>
          <input
            type="number"
            min="1"
            max="10"
            value={settings.suggestion_count || '3'}
            onChange={e => handleChange('suggestion_count', e.target.value)}
          />
          <span className="settings-hint">Number of daily recipe suggestions shown at the top of the Library</span>
        </div>
        <div className="settings-field">
          <label>Default Servings</label>
          <input
            type="number"
            min="1"
            max="20"
            value={settings.default_servings || '4'}
            onChange={e => handleChange('default_servings', e.target.value)}
          />
          <span className="settings-hint">Used as default when generating recipes or creating meal plans</span>
        </div>
        <div className="settings-field">
          <label>Max Tool Iterations</label>
          <input
            type="number"
            min="1"
            max="20"
            value={settings.max_tool_iterations || '5'}
            onChange={e => handleChange('max_tool_iterations', e.target.value)}
          />
          <span className="settings-hint">Maximum number of tool-use rounds per generation</span>
        </div>
        <div className="settings-field">
          <label>Generation Timeout (seconds)</label>
          <input
            type="number"
            min="10"
            max="600"
            value={settings.generation_timeout || '120'}
            onChange={e => handleChange('generation_timeout', e.target.value)}
          />
          <span className="settings-hint">HTTP timeout for each Ollama request</span>
        </div>
        <div className="settings-field">
          <label>UI Language</label>
          <select
            value={settings.ui_language || 'en'}
            onChange={e => handleChange('ui_language', e.target.value)}
          >
            <option value="en">English</option>
            <option value="nl">Dutch (Nederlands)</option>
          </select>
          <span className="settings-hint">
            Display language for the interface. Albert Heijn searches always use Dutch regardless of this setting.
            To use a dedicated model for translation, tag an Ollama provider with the <code>translation</code> tag.
          </span>
        </div>
        <button className="btn btn-primary" onClick={handleSave} disabled={saving}>
          {saving ? 'Saving...' : 'Save Settings'}
        </button>
      </div>
    </div>
  );
}
