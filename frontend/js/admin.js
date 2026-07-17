// Admin dashboard: revenue, orders across both channels, and live inventory.
import { formatKobo, apiFetch, toast } from './cart.js';
import { mountFooter } from './footer.js';
import { renderThemeToggle } from './theme.js';
import { initProductForm, openProductForm, renderProductsTable } from './products-admin.js';
import { currentUser, renderNav } from './auth.js';
import { connectLive, liveIndicator } from './live.js';

const $ = (id) => document.getElementById(id);

// How often to re-read as a backstop. Rare while the stream is healthy, brisk
// when it is not — a dashboard nobody can trust is worse than a few extra
// queries.
const SLOW_POLL_MS = 20000;
const FAST_POLL_MS = 5000;
let streamUp = false;

function stat(label, value, sub) {
  const d = document.createElement('div');
  d.className = 'stat';
  const l = document.createElement('span');
  l.className = 'label';
  l.textContent = label;
  const v = document.createElement('div');
  v.className = 'value';
  v.textContent = value;
  d.append(l, v);
  if (sub) {
    const s = document.createElement('span');
    s.className = 'sub';
    s.textContent = sub;
    d.append(s);
  }
  return d;
}

function badge(kind) {
  const b = document.createElement('span');
  b.className = `badge badge-${kind}`;
  b.textContent = kind === 'pos' ? 'Counter' : kind === 'online' ? 'Web' : kind;
  return b;
}

function renderStats(s) {
  const wrap = $('stats');
  wrap.setAttribute('aria-busy', 'false');
  wrap.innerHTML = '';
  wrap.append(
    stat('Confirmed revenue', formatKobo(s.totalRevenueKobo), 'Settled by webhook only'),
    stat('Paid orders', String(s.paidOrders), `${s.onlineOrders} web · ${s.posOrders} counter`),
    stat('Pending', String(s.pendingOrders), 'Awaiting confirmation'),
    stat('Failed', String(s.failedOrders), 'Not charged'),
    stat('Low stock', String(s.lowStockCount), '5 or fewer remaining')
  );
}

function renderOrders(orders) {
  const body = $('orders');
  body.innerHTML = '';

  if (!orders.length) {
    const tr = document.createElement('tr');
    const td = document.createElement('td');
    td.colSpan = 7;
    td.className = 'empty';
    td.textContent = 'No orders yet. Place one from the store or the counter.';
    tr.append(td);
    body.append(tr);
    return;
  }

  for (const o of orders) {
    const tr = document.createElement('tr');

    const ref = document.createElement('td');
    const code = document.createElement('code');
    code.className = 'ref';
    code.textContent = o.reference;
    ref.append(code);

    const cust = document.createElement('td');
    cust.textContent = o.customer;

    const chan = document.createElement('td');
    chan.append(badge(o.channel));

    const status = document.createElement('td');
    status.append(badge(o.status));

    const total = document.createElement('td');
    total.className = 'num';
    total.textContent = formatKobo(o.totalKobo);

    const when = document.createElement('td');
    when.textContent = new Date(o.createdAt).toLocaleString();

    const act = document.createElement('td');
    if (o.status === 'paid') {
      const btn = document.createElement('button');
      btn.className = 'btn btn-sm btn-secondary';
      btn.type = 'button';
      btn.textContent = 'Refund';
      btn.addEventListener('click', () => refundOrder(o.reference, o.totalKobo));
      act.append(btn);
    }

    tr.append(ref, cust, chan, status, total, when, act);
    body.append(tr);
  }
}


// Refunding is money leaving the business, so it asks first. A single
// mis-click on a table row must not be able to return someone's payment.
async function refundOrder(reference, totalKobo) {
  const reason = window.prompt(
    `Refund ${formatKobo(totalKobo)} for ${reference}?\n\nEnter a reason (required):`
  );
  if (reason === null) return;
  if (!reason.trim()) {
    toast('A refund reason is required');
    return;
  }

  try {
    const res = await apiFetch(`/api/admin/orders/${encodeURIComponent(reference)}/refund`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ reason: reason.trim() }),
    });
    const status = res.refund.status;
    toast(
      status === 'completed'
        ? 'Refund completed — customer credited'
        : `Refund ${status} with Monnify (${res.monnifyStatus})`
    );
    load();
  } catch (err) {
    toast(`Refund failed: ${err.message}`);
  }
}

async function load() {
  $('error').hidden = true;
  try {
    // Both requests are independent; fire them together rather than serially.
    // The admin list includes hidden products; the storefront list does not.
    const [overview, products] = await Promise.all([
      apiFetch('/api/admin/overview'),
      apiFetch('/api/admin/products'),
    ]);
    renderStats(overview.summary);
    renderOrders(overview.recent);
    renderProductsTable(products, $('inventory'));
    if (overview.refunds) renderRefunds(overview.refunds);
  } catch (err) {
    $('error').textContent = `Could not load the dashboard: ${err.message}`;
    $('error').hidden = false;
    $('stats').setAttribute('aria-busy', 'false');
  }
}

function renderRefunds(refunds) {
  const body = $('refunds');
  if (!body) return;
  body.innerHTML = '';

  if (!refunds.length) {
    const tr = document.createElement('tr');
    const td = document.createElement('td');
    td.colSpan = 5;
    td.className = 'empty';
    td.textContent = 'No refunds issued.';
    tr.append(td);
    body.append(tr);
    return;
  }

  for (const rf of refunds) {
    const tr = document.createElement('tr');

    const ref = document.createElement('td');
    const c = document.createElement('code');
    c.className = 'ref';
    c.textContent = rf.orderReference || rf.reference;
    ref.append(c);

    const amt = document.createElement('td');
    amt.className = 'num';
    amt.textContent = formatKobo(rf.amountKobo);

    const st = document.createElement('td');
    const b = document.createElement('span');
    b.className = `badge badge-${rf.status === 'completed' ? 'paid' : rf.status === 'failed' ? 'failed' : 'pending'}`;
    b.textContent = rf.status;
    st.append(b);

    const reason = document.createElement('td');
    reason.textContent = rf.reason;
    reason.style.whiteSpace = 'normal';

    const when = document.createElement('td');
    when.textContent = new Date(rf.createdAt).toLocaleString();

    tr.append(ref, amt, st, reason, when);
    body.append(tr);
  }
}

// Gate the page itself. The API already refuses non-admins, but rendering an
// admin shell to a stranger before the fetch fails looks like a hole even
// when it is not.
async function boot() {
  const user = await currentUser();
  renderNav(user, document.getElementById('account-nav'));
  if (!user || user.role !== 'admin') {
    window.location.href = '/login?next=/admin';
    return;
  }
  renderThemeToggle($('theme-toggle'));
  initProductForm();
  $('add-product').addEventListener('click', () => openProductForm(null));
  document.addEventListener('products:changed', load);
  $('refresh').addEventListener('click', load);
  await load();

  // Live updates. Each event is a nudge to re-read, not the data itself:
  // reloading is cheap and keeps one code path for rendering, so the screen
  // cannot drift from the database because of a missed frame.
  connectLive({
    onEvent: (ev) => {
      load();
      if (ev.type === 'order.paid') toast('Payment confirmed — a new order just settled');
      if (ev.type === 'refund.completed') toast('A refund completed');
    },
    onStatus: (connected) => {
      streamUp = connected;
      liveIndicator($('live-status'), connected);
    },
  });

  // Backstop.
  //
  // The stream is the fast path, not the only path. A dropped connection, a
  // sleeping instance, or a proxy that quietly closed an idle socket would
  // otherwise leave this dashboard showing "pending" against an order the
  // database already settled — and the only way to find out would be to hit
  // refresh, which is exactly what live updates were meant to remove.
  //
  // This is the same lesson as the webhook: never let one delivery mechanism
  // be the only way a screen learns the truth.
  // A self-scheduling timeout, not setInterval: the interval has to change
  // when the stream drops, and setInterval fixes its delay at creation time —
  // when streamUp is still false — so it would never adapt.
  const tick = () => {
    if (!document.hidden) load();
    setTimeout(tick, streamUp ? SLOW_POLL_MS : FAST_POLL_MS);
  };
  setTimeout(tick, streamUp ? SLOW_POLL_MS : FAST_POLL_MS);

  // Coming back to a tab should show the truth immediately, not on the next
  // tick — this is the moment a stale number is most likely to be believed.
  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) load();
  });
}

boot();

mountFooter();
