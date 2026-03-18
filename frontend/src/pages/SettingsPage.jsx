import { useState, useEffect } from 'react';
import {
  listProviders, createProvider, updateProvider, deleteProvider,
  getSettings, updateSettings,
} from '../api/client';

export default function SettingsPage() {
  return (
    <div className="settings-page">
      <h2>Settings</h2>
      <ProvidersSection />
      <GeneralSettings />
    </div>
  );
}

function ProvidersSection() {
  const [providers, setProviders] = useState([]);
  const [loading, setLoading] = useState(true);
  const [form, setForm] = useState({ name: '', host: '', model: '' });
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
    setForm({ name: '', host: '', model: '' });
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
                <input
                  value={editing.model}
                  onChange={e => setEditing({ ...editing, model: e.target.value })}
                  placeholder="Model"
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
          <input
            value={form.model}
            onChange={e => setForm({ ...form, model: e.target.value })}
            placeholder="Model (e.g. qwen3.5:9b)"
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
        <button className="btn btn-primary" onClick={handleSave} disabled={saving}>
          {saving ? 'Saving...' : 'Save Settings'}
        </button>
      </div>
    </div>
  );
}
