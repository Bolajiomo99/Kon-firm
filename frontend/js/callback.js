// Payment callback.
//
// Monnify redirects the customer here after checkout. The redirect itself
// proves nothing — it can be replayed, or typed by hand — so this page never
// declares success on its own. It polls the server, which reports only what
// the signature-verified webhook recorded.
import { formatKobo, apiFetch } from './cart.js';

const $ = (id) => document.getElementById(id);

// Monnify appends its own reference; fall back to the one we stashed at checkout.
const params = new URLSearchParams(window.location.search);
const ref =
  params.get('paymentReference') ||
  params.get('reference') ||
  safeSession('konfirm.lastRef');

function safeSession(key) {
  try {
    return sessionStorage.getItem(key);
  } catch {
    return null;
  }
}

function setState(headline, sub, badge) {
  const wrap = $('state');
  wrap.innerHTML = '';
  const box = document.createElement('div');
  box.className = 'empty';

  if (badge) {
    const b = document.createElement('span');
    b.className = `badge badge-${badge}`;
    b.textContent = badge;
    b.style.marginBottom = '12px';
    box.append(b);
  }

  const h = document.createElement('h2');
  h.textContent = headline;
  const p = document.createElement('p');
  p.textContent = sub;
  p.style.color = 'var(--text-muted)';

  box.append(h, p);
  wrap.append(box);
}

function renderReceipt(order) {
  const rows = [
    ['Reference', order.reference],
    ['Status', order.status],
    ['Customer', order.customerName],
    ['Email', order.customerEmail],
    ['Channel', order.channel],
    ['Total', formatKobo(order.totalKobo)],
  ];
  if (order.amountPaidKobo != null) rows.push(['Amount paid', formatKobo(order.amountPaidKobo)]);
  if (order.paymentMethod) rows.push(['Method', order.paymentMethod]);
  if (order.paidAt) rows.push(['Paid at', new Date(order.paidAt).toLocaleString()]);
  if (order.transactionRef) rows.push(['Monnify reference', order.transactionRef]);

  const body = $('receipt-body');
  body.innerHTML = '';

  for (const [k, v] of rows) {
    const tr = document.createElement('tr');
    const th = document.createElement('th');
    th.scope = 'row';
    th.textContent = k;
    const td = document.createElement('td');
    td.textContent = v;
    td.style.whiteSpace = 'normal';
    tr.append(th, td);
    body.append(tr);
  }

  if (order.items && order.items.length) {
    for (const it of order.items) {
      const tr = document.createElement('tr');
      const th = document.createElement('th');
      th.scope = 'row';
      th.textContent = `${it.quantity} × ${it.productName}`;
      const td = document.createElement('td');
      td.textContent = formatKobo(it.unitPriceKobo * it.quantity);
      tr.append(th, td);
      body.append(tr);
    }
  }

  $('receipt').hidden = false;
}

// Settlement is asynchronous: the customer can land here before Monnify's
// webhook arrives. Poll with a bounded budget rather than guessing a status.
const POLL_MS = 2000;
const MAX_ATTEMPTS = 15; // ~30 seconds

async function poll(attempt = 0) {
  if (!ref) {
    setState('No order reference found', 'Return to the store and try again.', 'failed');
    return;
  }

  let order;
  try {
    order = await apiFetch(`/api/orders/${encodeURIComponent(ref)}`);
  } catch (err) {
    setState('Could not load this order', err.message, 'failed');
    return;
  }

  renderReceipt(order);

  if (order.status === 'paid') {
    setState('Payment confirmed', 'Monnify confirmed this payment and the order is settled.', 'paid');
    return;
  }
  if (order.status === 'failed' || order.status === 'expired') {
    setState('Payment not completed', 'This transaction did not succeed. Nothing was charged.', order.status);
    return;
  }

  if (attempt >= MAX_ATTEMPTS) {
    setState(
      'Still awaiting confirmation',
      'Monnify has not confirmed this payment yet. The order stays pending until it does — refresh in a moment.',
      'pending'
    );
    return;
  }

  setState('Confirming your payment…', `Waiting for Monnify’s confirmation (${attempt + 1}/${MAX_ATTEMPTS})…`, 'pending');
  setTimeout(() => poll(attempt + 1), POLL_MS);
}

poll();
