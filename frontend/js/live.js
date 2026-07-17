// Live updates over Server-Sent Events.
//
// EventSource reconnects by itself when a connection drops, which is the main
// reason this is SSE and not a WebSocket: no keepalive logic, no backoff to
// write, no library.
//
// Every handler here is a hint to refresh, not a source of truth. The page
// still rebuilds its state from the API, so a missed event costs a few seconds
// of staleness rather than a wrong number on screen.

/**
 * Opens a live stream.
 * @param {object} opts
 * @param {string} [opts.order] - watch one order; omit for the admin firehose
 * @param {(ev: object) => void} opts.onEvent
 * @param {(connected: boolean) => void} [opts.onStatus]
 * @returns {() => void} close
 */
export function connectLive({ order, onEvent, onStatus } = {}) {
  const url = order ? `/api/stream?order=${encodeURIComponent(order)}` : '/api/stream';
  let source;
  let closed = false;

  const open = () => {
    if (closed) return;
    source = new EventSource(url);

    source.addEventListener('ready', () => onStatus?.(true));

    for (const type of [
      'order.created', 'order.paid', 'order.failed',
      'refund.issued', 'refund.completed', 'stock.changed',
    ]) {
      source.addEventListener(type, (e) => {
        try {
          onEvent({ type, ...JSON.parse(e.data) });
        } catch {
          // A malformed frame must not kill the stream.
        }
      });
    }

    source.onerror = () => {
      onStatus?.(false);
      // EventSource retries on its own; do not close it here or that stops.
    };
  };

  open();

  return () => {
    closed = true;
    onStatus?.(false);
    source?.close();
  };
}

/** Renders a small live/offline indicator. */
export function liveIndicator(el, connected) {
  if (!el) return;
  el.textContent = connected ? 'Live' : 'Reconnecting…';
  el.style.color = connected ? 'var(--green-600)' : 'var(--text-muted)';
  const dot = el.previousElementSibling;
  if (dot && dot.classList.contains('dot')) {
    dot.style.background = connected ? 'var(--green-600)' : 'var(--text-muted)';
  }
}
