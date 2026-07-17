import { formatKobo, apiFetch } from './cart.js';
import { renderThemeToggle } from './theme.js';
import { mountFooter } from './footer.js';
import { currentUser, renderNav, requireLogin } from './auth.js';

function badge(kind) {
  const b = document.createElement('span');
  b.className = `badge badge-${kind}`;
  b.textContent = kind;
  return b;
}

const STATUS_COPY = {
  paid: 'Confirmed by Monnify — being prepared for dispatch.',
  pending: 'Waiting for Monnify to confirm your payment.',
  failed: 'This payment did not go through. You were not charged.',
  expired: 'The checkout window closed before payment.',
  refunded: 'Refunded — the money is on its way back to you.',
};

function renderStats(orders) {
  const wrap = document.getElementById('order-stats');
  wrap.innerHTML = '';
  const paid = orders.filter((o) => o.status === 'paid');
  const spent = paid.reduce((n, o) => n + o.totalKobo, 0);

  const stat = (label, value, sub) => {
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
      const s2 = document.createElement('span');
      s2.className = 'sub';
      s2.textContent = sub;
      d.append(s2);
    }
    return d;
  };

  wrap.append(
    stat('Orders', String(orders.length), `${paid.length} confirmed`),
    stat('Total spent', formatKobo(spent), 'Confirmed payments only'),
    stat('Awaiting', String(orders.filter((o) => o.status === 'pending').length), 'Not yet confirmed')
  );
}

/** One card per order — a table row cannot hold a receipt. */
function renderOrders(orders) {
  const list = document.getElementById('orders-list');
  list.innerHTML = '';

  if (!orders.length) {
    const box = document.createElement('div');
    box.className = 'auth-card';
    box.style.textAlign = 'center';
    const h = document.createElement('h3');
    h.textContent = 'No orders yet';
    const p = document.createElement('p');
    p.style.color = 'var(--text-muted)';
    p.textContent = 'When you buy something, it will appear here with its receipt.';
    const a = document.createElement('a');
    a.className = 'btn';
    a.href = '/#catalogue';
    a.textContent = 'Start shopping';
    box.append(h, p, a);
    list.append(box);
    return;
  }

  for (const o of orders) {
    const card = document.createElement('article');
    card.className = 'order-card';

    const head = document.createElement('div');
    head.className = 'order-head';
    const left = document.createElement('div');
    const ref = document.createElement('code');
    ref.className = 'ref';
    ref.textContent = o.reference;
    const when = document.createElement('div');
    when.style.cssText = 'font-size:.8rem;color:var(--text-muted);margin-top:2px';
    when.textContent = new Date(o.createdAt).toLocaleString('en-NG', {
      dateStyle: 'medium', timeStyle: 'short',
    });
    left.append(ref, when);
    head.append(left, badge(o.status));

    const body = document.createElement('div');
    body.className = 'order-body';
    const note = document.createElement('p');
    note.className = 'order-note';
    note.textContent = STATUS_COPY[o.status] || '';
    body.append(note);

    const rows = document.createElement('div');
    rows.className = 'order-lines';
    const add = (k, v, strong) => {
      const r = document.createElement('div');
      r.className = 'order-line' + (strong ? ' strong' : '');
      const a = document.createElement('span');
      a.textContent = k;
      const b = document.createElement('span');
      b.textContent = v;
      r.append(a, b);
      rows.append(r);
    };

    const q = o.quote || {};
    if (q.subtotalKobo) add('Subtotal', formatKobo(q.subtotalKobo));
    if (q.discountKobo > 0) add(`Discount${q.voucherCode ? ' (' + q.voucherCode + ')' : ''}`, '−' + formatKobo(q.discountKobo));
    if (q.subtotalKobo) add('Delivery', q.deliveryFeeKobo > 0 ? formatKobo(q.deliveryFeeKobo) : 'Free');
    add('Total', formatKobo(o.totalKobo), true);
    if (q.vatKobo) {
      const v = document.createElement('p');
      v.className = 'vat-note';
      v.style.textAlign = 'left';
      v.textContent = `Includes VAT of ${formatKobo(q.vatKobo)} at ${(q.vatRateBp / 100)}%`;
      rows.append(v);
    }
    body.append(rows);

    if (o.deliveryAddress) {
      const d = document.createElement('p');
      d.className = 'order-note';
      d.style.marginTop = '10px';
      d.textContent = `🚚 ${o.deliveryAddress}${o.deliveryCity ? ', ' + o.deliveryCity : ''}${o.deliveryState ? ', ' + o.deliveryState : ''}`;
      body.append(d);
    }
    if (o.paymentMethod) {
      const m = document.createElement('p');
      m.className = 'order-note';
      m.textContent = `💳 ${o.paymentMethod.replace(/_/g, ' ').toLowerCase()}`;
      body.append(m);
    }

    const foot = document.createElement('div');
    foot.className = 'order-foot';
    const link = document.createElement('a');
    link.className = 'btn btn-sm btn-secondary';
    link.href = `/payment/callback?paymentReference=${encodeURIComponent(o.reference)}`;
    link.textContent = 'View receipt';
    foot.append(link);

    card.append(head, body, foot);
    list.append(card);
  }
}

async function load() {
  renderThemeToggle(document.getElementById('theme-toggle'));
  const user = await currentUser();
  renderNav(user, document.getElementById('account-nav'));
  if (!user) return requireLogin('/orders');

  try {
    const orders = await apiFetch('/api/me/orders');
    renderStats(orders);
    renderOrders(orders);
  } catch (err) {
    document.getElementById('error').textContent = err.message;
    document.getElementById('error').hidden = false;
  }
}

load();
mountFooter();
