// Shared cart state, used by both the storefront and the POS counter.
//
// The cart holds product IDs and quantities only. It deliberately does not
// hold prices: the server prices every order at checkout. Prices here are for
// display, and are never sent anywhere.

const KEY = 'konfirm.cart.v1';

/** Format kobo (integer minor units) as naira. Never uses floats. */
export function formatKobo(kobo) {
  const n = Number(kobo);
  const sign = n < 0 ? '-' : '';
  const abs = Math.abs(n);
  const major = Math.floor(abs / 100);
  const minor = String(abs % 100).padStart(2, '0');
  return `${sign}₦${major.toLocaleString('en-NG')}.${minor}`;
}

export class Cart {
  constructor(storageKey = KEY) {
    this.key = storageKey;
    this.lines = this.#load();
    this.listeners = new Set();
  }

  #load() {
    try {
      const raw = localStorage.getItem(this.key);
      if (!raw) return [];
      const parsed = JSON.parse(raw);
      // Defend against a hand-edited or stale localStorage payload: a bad
      // shape here would otherwise throw on every render.
      if (!Array.isArray(parsed)) return [];
      return parsed.filter(
        (l) => l && Number.isFinite(l.productId) && Number.isInteger(l.quantity) && l.quantity > 0
      );
    } catch {
      return [];
    }
  }

  #save() {
    try {
      localStorage.setItem(this.key, JSON.stringify(this.lines));
    } catch {
      // Private browsing can refuse writes. The cart still works in memory.
    }
    this.listeners.forEach((fn) => fn(this));
  }

  onChange(fn) {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  add(product, qty = 1) {
    const existing = this.lines.find((l) => l.productId === product.id);
    const inCart = existing ? existing.quantity : 0;

    // Never let the cart exceed stock; the server would reject it anyway, but
    // failing here is faster and explains itself.
    if (inCart + qty > product.stock) {
      return { ok: false, reason: `Only ${product.stock} in stock` };
    }

    if (existing) {
      existing.quantity += qty;
    } else {
      // Name, price and image are cached for display only. The server prices
      // the order from product IDs at checkout, so a stale copy here can make
      // the bag look wrong for a moment — never make the charge wrong.
      this.lines.push({
        productId: product.id,
        quantity: qty,
        name: product.name,
        priceKobo: product.priceKobo,
        stock: product.stock,
        imageUrl: product.imageUrl || '',
      });
    }
    this.#save();
    return { ok: true };
  }

  setQuantity(productId, qty) {
    const line = this.lines.find((l) => l.productId === productId);
    if (!line) return;
    if (qty <= 0) return this.remove(productId);
    if (qty > line.stock) return;
    line.quantity = qty;
    this.#save();
  }

  remove(productId) {
    this.lines = this.lines.filter((l) => l.productId !== productId);
    this.#save();
  }

  clear() {
    this.lines = [];
    this.#save();
  }

  get count() {
    return this.lines.reduce((n, l) => n + l.quantity, 0);
  }

  /** Display total only. The server computes the authoritative figure. */
  get totalKobo() {
    return this.lines.reduce((n, l) => n + l.priceKobo * l.quantity, 0);
  }

  get isEmpty() {
    return this.lines.length === 0;
  }

  /** The payload shape the API accepts: IDs and quantities, no prices. */
  toItems() {
    return this.lines.map((l) => ({ productId: l.productId, quantity: l.quantity }));
  }
}

/** Small toast helper shared across pages. */
export function toast(message, ms = 2600) {
  const el = document.getElementById('toast');
  if (!el) return;
  el.textContent = message;
  el.dataset.show = 'true';
  clearTimeout(el._t);
  el._t = setTimeout(() => {
    el.dataset.show = 'false';
  }, ms);
}

/** Fetch JSON, turning non-2xx into a thrown Error carrying the API message. */
export async function apiFetch(url, options) {
  const res = await fetch(url, options);
  const text = await res.text();
  let body = null;
  if (text) {
    try {
      body = JSON.parse(text);
    } catch {
      // Non-JSON error page; fall through to a generic message.
    }
  }
  if (!res.ok) {
    throw new Error((body && body.error) || `Request failed (${res.status})`);
  }
  return body;
}

/* ---------- Pending order recovery ---------- */

// Remembering an order across the trip to Monnify.
//
// sessionStorage dies with the tab, and Monnify's page is a full navigation
// away — if the customer closes it, opens the shop fresh, or the redirect
// never fires, the reference is gone and a guest has no other way to find
// their receipt. localStorage survives all of that.
//
// The receipt itself is never trusted to this: it only holds a reference, and
// the server still decides whether that order was paid.
const PENDING_KEY = 'konfirm.pendingOrder';

export function rememberOrder(reference) {
  try {
    localStorage.setItem(PENDING_KEY, JSON.stringify({ reference, at: Date.now() }));
    sessionStorage.setItem('konfirm.lastRef', reference); // fast path for the callback page
  } catch { /* private browsing */ }
}

export function recallOrder() {
  try {
    const raw = localStorage.getItem(PENDING_KEY);
    if (!raw) return null;
    const o = JSON.parse(raw);
    // A checkout URL is valid for 40 minutes; after a day this is stale and
    // nagging about it is worse than forgetting it.
    if (!o.reference || Date.now() - o.at > 24 * 60 * 60 * 1000) {
      localStorage.removeItem(PENDING_KEY);
      return null;
    }
    return o.reference;
  } catch {
    return null;
  }
}

export function forgetOrder() {
  try {
    localStorage.removeItem(PENDING_KEY);
  } catch { /* nothing to do */ }
}
