import { useState, useEffect } from 'react';

// Distinct border colors for up to 10 concurrent batch slots.
const SLOT_COLORS = [
  '#b5451b', // terracotta (primary)
  '#1e7c4a', // green
  '#1d4ed8', // blue
  '#7c3aed', // violet
  '#0891b2', // cyan
  '#d97706', // amber
  '#db2777', // pink
  '#059669', // emerald
  '#475569', // slate
  '#9333ea', // purple
];

export default function GenerationProgress({ events, loading, hasRecipes }) {
  const [collapsed, setCollapsed] = useState(false);

  useEffect(() => {
    if (loading) setCollapsed(false);
  }, [loading]);

  useEffect(() => {
    if (hasRecipes && !loading) setCollapsed(true);
  }, [hasRecipes, loading]);

  if (events.length === 0) return null;

  const lastEvent = events[events.length - 1];
  const isActive = lastEvent.type !== 'error' && loading;
  const isBatch = events.some(e => e.index > 0);

  return (
    <div className={`generation-progress ${collapsed ? 'collapsed' : ''}`}>
      <button type="button" className="progress-header" onClick={() => setCollapsed(c => !c)}>
        <h3>Generation Progress</h3>
        {isActive && <span className="loading-dots"><span /><span /><span /></span>}
        <span className="progress-toggle">{collapsed ? '\u25B6' : '\u25BC'}</span>
      </button>
      {!collapsed && (
        <div className="events-list">
          {events.map((event, i) => {
            const slotColor = isBatch ? SLOT_COLORS[(event.index ?? 0) % SLOT_COLORS.length] : null;
            return (
              <div
                key={i}
                className={`event event-${event.type}`}
                style={slotColor ? { borderLeftColor: slotColor } : undefined}
              >
                {isBatch && (
                  <span className="event-slot-label" style={{ color: slotColor }}>
                    #{(event.index ?? 0) + 1}
                  </span>
                )}
                {event.type === 'status' && <span className="event-status">{event.message}</span>}
                {event.type === 'tool_call' && (
                  <span className="event-tool">Calling tool: <strong>{event.tool}</strong></span>
                )}
                {event.type === 'tool_result' && (
                  <span className="event-result">Got results from: <strong>{event.tool}</strong></span>
                )}
                {event.type === 'error' && (
                  <span className="event-error">Error: {event.message}</span>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
