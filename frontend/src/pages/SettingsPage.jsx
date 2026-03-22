import { useState, useEffect } from 'react';
import {
  listProviders, createProvider, updateProvider, deleteProvider,
  getSettings, updateSettings, listModels,
} from '../api/client';

export default function SettingsPage() {
  return (
    <div className="settings-page">
      <h2>Settings</h2>
      <ProvidersSection />
      <GeneralSettings />
      <BackgroundGenerationSettings />
    </div>
  );
}

const KNOWN_TAGS = ['background', 'review'];

function TagPicker({ tags = [], onChange }) {
  const [selected, setSelected] = useState('');
  const [custom, setCustom] = useState('');

  const add = () => {
    const value = selected === '__custom__' ? custom.trim() : selected;
    if (!value || tags.includes(value)) return;
    onChange([...tags, value]);
    setSelected('');
    setCustom('');
  };

  const remove = (tag) => onChange(tags.filter(t => t !== tag));

  const handleKeyDown = (e) => {
    if (e.key === 'Enter') { e.preventDefault(); add(); }
  };

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
          onChange={e => { setSelected(e.target.value); setCustom(''); }}
        >
          <option value="">Add tag…</option>
          {availableKnown.map(t => <option key={t} value={t}>{t}</option>)}
          <option value="__custom__">Custom…</option>
        </select>
        {selected === '__custom__' && (
          <input
            type="text"
            value={custom}
            onChange={e => setCustom(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Tag name"
            autoFocus
          />
        )}
        <button
          type="button"
          className="btn btn-secondary"
          onClick={add}
          disabled={!selected || (selected === '__custom__' && !custom.trim())}
        >
          Add
        </button>
      </div>
    </div>
  );
}

function ModelInput({ host, value, onChange, id }) {
  const [models, setModels] = useState([]);

  useEffect(() => {
    if (!host) { setModels([]); return; }
    let cancelled = false;
    listModels(host)
      .then(data => { if (!cancelled) setModels(data || []); })
      .catch(() => { if (!cancelled) setModels([]); });
    return () => { cancelled = true; };
  }, [host]);

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
  const [form, setForm] = useState({ name: '', host: '', model: '', tags: [] });
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
    setForm({ name: '', host: '', model: '', tags: [] });
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
      <h3>Ollama Providers</h3>
      <p className="settings-description">
        Configure multiple Ollama endpoints. Batch generation distributes work across enabled providers.
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
                <ModelInput
                  host={editing.host}
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
          <ModelInput
            host={form.host}
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

function BackgroundGenerationSettings() {
  const [settings, setSettings] = useState({
    background_generation_enabled: 'false',
    background_generation_interval: '86400',
    background_generation_count: '1',
    background_generation_max_retries: '3',
    background_generation_time: '',
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
        background_generation_interval: settings.background_generation_interval,
        background_generation_count: settings.background_generation_count,
        background_generation_max_retries: settings.background_generation_max_retries,
        background_generation_time: settings.background_generation_time,
      });
    } catch (err) {
      alert('Failed to save: ' + err.message);
    } finally {
      setSaving(false);
    }
  };

  if (loading) return null;

  const enabled = settings.background_generation_enabled === 'true';
  const intervals = [
    { label: '30 minutes', value: '1800' },
    { label: '1 hour', value: '3600' },
    { label: '2 hours', value: '7200' },
    { label: '6 hours', value: '21600' },
    { label: '12 hours', value: '43200' },
    { label: '24 hours', value: '86400' },
    { label: '2 days', value: '172800' },
    { label: '1 week', value: '604800' },
  ];

  return (
    <div className="settings-section">
      <h3>Background Generation</h3>
      <p className="settings-description">
        Automatically generate recipes on a schedule. Use the <code>background</code> tag on a provider (e.g. your always-on server)
        to prefer it for background tasks.
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
              <label>Interval</label>
              <select
                value={settings.background_generation_interval}
                onChange={e => setSettings(s => ({ ...s, background_generation_interval: e.target.value }))}
              >
                {intervals.map(i => <option key={i.value} value={i.value}>{i.label}</option>)}
              </select>
              <span className="settings-hint">Minimum time between runs</span>
            </div>
            <div className="settings-field">
              <label>Time of day</label>
              <input
                type="time"
                value={settings.background_generation_time}
                onChange={e => setSettings(s => ({ ...s, background_generation_time: e.target.value }))}
              />
              <span className="settings-hint">
                Only generate at this time of day (leave empty to run as soon as the interval elapses)
              </span>
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
              <span className="settings-hint">Number of recipes generated each interval</span>
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

  const handleSave = async () => {
    setSaving(true);
    try {
      await updateSettings(settings);
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
            To use a dedicated model for translation, tag an Ollama provider with <code>translation</code>.
          </span>
        </div>
        <button className="btn btn-primary" onClick={handleSave} disabled={saving}>
          {saving ? 'Saving...' : 'Save Settings'}
        </button>
      </div>
    </div>
  );
}
