import { useState, useEffect, useRef, useCallback } from 'react';
import { listInventory, createInventoryItem, updateInventoryItem, deleteInventoryItem,
         scanIngredient, listPendingScans, deletePendingScan } from '../api/client';

const EMPTY_FORM = { name: '', amount: '', unit: '', notes: '' };
let nextLocalId = 1; // local-only IDs for in-flight placeholders

function ItemForm({ initial = EMPTY_FORM, onSave, onCancel, saving }) {
  const [form, setForm] = useState(initial);
  const set = (f, v) => setForm(prev => ({ ...prev, [f]: v }));

  useEffect(() => { setForm(initial); }, [initial.name, initial.amount, initial.unit, initial.notes]);

  const handleSubmit = (e) => {
    e.preventDefault();
    if (!form.name.trim()) return;
    onSave({
      name: form.name.trim(),
      amount: parseFloat(form.amount) || 0,
      unit: form.unit.trim(),
      notes: form.notes.trim(),
    });
  };

  return (
    <form className="inventory-item-form" onSubmit={handleSubmit}>
      <div className="inventory-form-row">
        <div className="form-group" style={{ flex: 2 }}>
          <label>Name *</label>
          <input className="edit-input" value={form.name} onChange={e => set('name', e.target.value)}
            placeholder="e.g. whole milk" autoFocus />
        </div>
        <div className="form-group" style={{ flex: 1 }}>
          <label>Amount</label>
          <input className="edit-input" type="number" min="0" step="any" value={form.amount}
            onChange={e => set('amount', e.target.value)} placeholder="0" />
        </div>
        <div className="form-group" style={{ flex: 1 }}>
          <label>Unit</label>
          <input className="edit-input" value={form.unit} onChange={e => set('unit', e.target.value)}
            placeholder="g, ml, pcs…" />
        </div>
      </div>
      <div className="form-group">
        <label>Notes</label>
        <input className="edit-input" value={form.notes} onChange={e => set('notes', e.target.value)}
          placeholder="optional" />
      </div>
      <div className="inventory-form-actions">
        <button className="btn btn-primary" type="submit" disabled={saving || !form.name.trim()}>
          {saving ? 'Saving…' : 'Save'}
        </button>
        {onCancel && <button className="btn btn-secondary" type="button" onClick={onCancel}>Cancel</button>}
      </div>
    </form>
  );
}

function ScanPanel({ onQueued }) {
  const inputRef = useRef();

  const handleFiles = (files) => {
    for (const file of files) {
      if (file.type.startsWith('image/')) onQueued(file);
    }
  };

  return (
    <div className="scan-panel">
      <div
        className="scan-dropzone"
        onClick={() => inputRef.current?.click()}
        onDragOver={e => e.preventDefault()}
        onDrop={e => { e.preventDefault(); handleFiles(Array.from(e.dataTransfer.files)); }}
      >
        <div className="scan-placeholder">
          <span className="scan-icon">📷</span>
          <span>Drop photos or click to upload</span>
          <span className="scan-hint">Upload multiple — AI scans run in the background</span>
        </div>
      </div>
      <input ref={inputRef} type="file" accept="image/*" capture="environment" multiple style={{ display: 'none' }}
        onChange={e => { handleFiles(Array.from(e.target.files)); e.target.value = ''; }} />
    </div>
  );
}

// Represents one backend-persisted pending scan (has a real DB id).
function PendingScanItem({ scan, onAdd, onDismiss }) {
  const initial = {
    name: scan.name || '',
    amount: scan.amount > 0 ? String(scan.amount) : '',
    unit: scan.unit || '',
    notes: scan.confident ? '' : '⚠ Low confidence — please verify',
  };
  const [form, setForm] = useState(initial);
  const set = (f, v) => setForm(prev => ({ ...prev, [f]: v }));
  const [saving, setSaving] = useState(false);

  const handleAdd = async (e) => {
    e.preventDefault();
    if (!form.name.trim()) return;
    setSaving(true);
    try {
      await onAdd({
        name: form.name.trim(),
        amount: parseFloat(form.amount) || 0,
        unit: form.unit.trim(),
        notes: form.notes.trim(),
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <li className="pending-scan-item">
      <div className="pending-scan-header">
        <div className="pending-scan-status">
          <span className="pending-scan-label">{!scan.confident ? 'Review — low confidence' : 'Detected'}</span>
        </div>
        <button className="btn btn-secondary btn-sm" onClick={() => onDismiss(scan.id)}>Dismiss</button>
      </div>
      <form className="inventory-item-form" onSubmit={handleAdd}>
        <div className="inventory-form-row">
          <div className="form-group" style={{ flex: 2 }}>
            <label>Name *</label>
            <input className="edit-input" value={form.name} onChange={e => set('name', e.target.value)} placeholder="e.g. whole milk" />
          </div>
          <div className="form-group" style={{ flex: 1 }}>
            <label>Amount</label>
            <input className="edit-input" type="number" min="0" step="any" value={form.amount} onChange={e => set('amount', e.target.value)} placeholder="0" />
          </div>
          <div className="form-group" style={{ flex: 1 }}>
            <label>Unit</label>
            <input className="edit-input" value={form.unit} onChange={e => set('unit', e.target.value)} placeholder="g, ml, pcs…" />
          </div>
        </div>
        <div className="inventory-form-actions">
          <button className="btn btn-primary" type="submit" disabled={saving || !form.name.trim()}>
            {saving ? 'Adding…' : 'Add to Inventory'}
          </button>
        </div>
      </form>
    </li>
  );
}

export default function InventoryPage() {
  const [items, setItems] = useState([]);
  const [loadingInventory, setLoadingInventory] = useState(true);
  const [pendingScans, setPendingScans] = useState([]); // backend-persisted
  const [inFlight, setInFlight] = useState([]); // local placeholders while upload is processing
  const [mode, setMode] = useState('manual');
  const [saving, setSaving] = useState(false);
  const [editingId, setEditingId] = useState(null);

  const loadPendingScans = useCallback(() => {
    listPendingScans()
      .then(data => setPendingScans(data || []))
      .catch(() => {});
  }, []);

  useEffect(() => {
    listInventory()
      .then(data => setItems(data || []))
      .catch(() => {})
      .finally(() => setLoadingInventory(false));
    loadPendingScans();
  }, []);

  const handleQueued = useCallback((file) => {
    const localId = nextLocalId++;
    setInFlight(prev => [...prev, { localId, error: null }]);

    scanIngredient(file)
      .then(async (res) => {
        if (!res.ok) {
          const err = await res.json().catch(() => ({ error: res.statusText }));
          throw new Error(err.error || 'Scan failed');
        }
        return res.json();
      })
      .then(() => {
        setInFlight(prev => prev.filter(f => f.localId !== localId));
        loadPendingScans();
      })
      .catch((err) => {
        setInFlight(prev => prev.map(f => f.localId !== localId ? f : { ...f, error: err.message }));
      });
  }, [loadPendingScans]);

  const handleAdd = async (data) => {
    setSaving(true);
    try {
      const created = await createInventoryItem(data);
      setItems(prev => [...prev, created].sort((a, b) => a.name.localeCompare(b.name)));
    } catch (err) {
      alert('Failed to save: ' + err.message);
    } finally {
      setSaving(false);
    }
  };

  const handleUpdate = async (id, data) => {
    setSaving(true);
    try {
      const updated = await updateInventoryItem(id, data);
      setItems(prev => prev.map(it => it.id === id ? updated : it));
      setEditingId(null);
    } catch (err) {
      alert('Failed to update: ' + err.message);
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (id) => {
    if (!confirm('Remove this item from inventory?')) return;
    try {
      await deleteInventoryItem(id);
      setItems(prev => prev.filter(it => it.id !== id));
    } catch (err) {
      alert('Failed to delete: ' + err.message);
    }
  };

  const handlePendingAdd = async (scanId, data) => {
    const created = await createInventoryItem(data);
    setItems(prev => [...prev, created].sort((a, b) => a.name.localeCompare(b.name)));
    await deletePendingScan(scanId);
    setPendingScans(prev => prev.filter(s => s.id !== scanId));
  };

  const handleDismiss = async (scanId) => {
    await deletePendingScan(scanId);
    setPendingScans(prev => prev.filter(s => s.id !== scanId));
  };

  const totalPending = pendingScans.length + inFlight.length;

  return (
    <div className="inventory-page">
      <div className="inventory-add-card">
        <div className="inventory-add-header">
          <h3>Add Ingredient</h3>
          <div className="mode-toggle">
            <button type="button" className={mode === 'manual' ? 'active' : ''} onClick={() => setMode('manual')}>Manual</button>
            <button type="button" className={mode === 'scan' ? 'active' : ''} onClick={() => setMode('scan')}>Scan Photo</button>
          </div>
        </div>

        {mode === 'scan' && <ScanPanel onQueued={handleQueued} />}

        {mode === 'manual' && (
          <ItemForm initial={EMPTY_FORM} onSave={handleAdd} saving={saving} />
        )}
      </div>

      {totalPending > 0 && (
        <div className="inventory-list-section">
          <h3>Pending Scans <span className="inventory-count">({totalPending})</span></h3>
          <ul className="inventory-list">
            {inFlight.map(f => (
              <li key={`local-${f.localId}`} className="pending-scan-item">
                <div className="pending-scan-header">
                  <div className="pending-scan-status">
                    {f.error
                      ? <span className="pending-scan-error">{f.error}</span>
                      : <><span className="scan-spinner" /><span>Scanning…</span></>}
                  </div>
                  {f.error && (
                    <button className="btn btn-secondary btn-sm"
                      onClick={() => setInFlight(prev => prev.filter(x => x.localId !== f.localId))}>
                      Dismiss
                    </button>
                  )}
                </div>
              </li>
            ))}
            {pendingScans.map(scan => (
              <PendingScanItem
                key={scan.id}
                scan={scan}
                onAdd={(data) => handlePendingAdd(scan.id, data)}
                onDismiss={handleDismiss}
              />
            ))}
          </ul>
        </div>
      )}

      <div className="inventory-list-section">
        <h3>Your Inventory {!loadingInventory && <span className="inventory-count">({items.length})</span>}</h3>
        {loadingInventory ? (
          <p className="text-secondary">Loading…</p>
        ) : items.length === 0 ? (
          <p className="text-secondary">No ingredients yet. Add some above.</p>
        ) : (
          <ul className="inventory-list">
            {items.map(item => (
              <li key={item.id} className="inventory-item">
                {editingId === item.id ? (
                  <ItemForm
                    initial={{ name: item.name, amount: item.amount > 0 ? String(item.amount) : '', unit: item.unit, notes: item.notes }}
                    onSave={data => handleUpdate(item.id, data)}
                    onCancel={() => setEditingId(null)}
                    saving={saving}
                  />
                ) : (
                  <div className="inventory-item-row">
                    <div className="inventory-item-info">
                      <span className="inventory-item-name">{item.name}</span>
                      {(item.amount > 0 || item.unit) && (
                        <span className="inventory-item-amount">
                          {item.amount > 0 ? item.amount : ''} {item.unit}
                        </span>
                      )}
                      {item.notes && <span className="inventory-item-notes">{item.notes}</span>}
                    </div>
                    <div className="inventory-item-actions">
                      <button className="btn btn-secondary btn-sm" onClick={() => setEditingId(item.id)}>Edit</button>
                      <button className="btn btn-danger btn-sm" onClick={() => handleDelete(item.id)}>Remove</button>
                    </div>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
