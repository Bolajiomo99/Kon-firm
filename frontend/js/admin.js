// Admin dashboard: revenue, orders across both channels, and live inventory.
import { formatKobo, apiFetch } from './cart.js';

const $ = (id) => document.getElementById(id);

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
    td.colSpan = 6;
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

    tr.append(ref, cust, chan, status, total, when);
    body.append(tr);
  }
}

function renderInventory(products) {
  const body = $('inventory');
  body.innerHTML = '';

  if (!products.length) {
    const tr = document.createElement('tr');
    const td = document.createElement('td');
    td.colSpan = 5;
    td.className = 'empty';
    td.textContent = 'No products.';
    tr.append(td);
    body.append(tr);
    return;
  }

  for (const p of products) {
    const tr = document.createElement('tr');

    const sku = document.createElement('td');
    const code = document.createElement('code');
    code.className = 'ref';
    code.textContent = p.sku;
    sku.append(code);

    const name = document.createElement('td');
    name.textContent = p.name;

    const bar = document.createElement('td');
    const bc = document.createElement('code');
    bc.className = 'ref';
    bc.textContent = p.barcode || '—';
    bar.append(bc);

    const price = document.createElement('td');
    price.className = 'num';
    price.textContent = formatKobo(p.priceKobo);

    const stock = document.createElement('td');
    stock.className = 'num';
    const s = document.createElement('span');
    s.textContent = String(p.stock);
    if (p.stock <= 0) s.className = 'stock out';
    else if (p.stock <= 5) s.className = 'stock low';
    stock.append(s);

    tr.append(sku, name, bar, price, stock);
    body.append(tr);
  }
}

async function load() {
  $('error').hidden = true;
  try {
    // Both requests are independent; fire them together rather than serially.
    const [overview, products] = await Promise.all([
      apiFetch('/api/admin/overview'),
      apiFetch('/api/products'),
    ]);
    renderStats(overview.summary);
    renderOrders(overview.recent);
    renderInventory(products);
  } catch (err) {
    $('error').textContent = `Could not load the dashboard: ${err.message}`;
    $('error').hidden = false;
    $('stats').setAttribute('aria-busy', 'false');
  }
}

$('refresh').addEventListener('click', load);
load();
