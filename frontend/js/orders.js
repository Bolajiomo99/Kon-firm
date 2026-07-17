import { formatKobo, apiFetch } from './cart.js';
import { mountFooter } from './footer.js';
import { currentUser, renderNav, requireLogin } from './auth.js';

function badge(kind) {
  const b = document.createElement('span');
  b.className = `badge badge-${kind}`;
  b.textContent = kind;
  return b;
}

async function load() {
  const user = await currentUser();
  renderNav(user, document.getElementById('account-nav'));
  if (!user) return requireLogin('/orders');

  try {
    const orders = await apiFetch('/api/me/orders');
    const body = document.getElementById('orders');
    body.innerHTML = '';

    if (!orders.length) {
      const tr = document.createElement('tr');
      const td = document.createElement('td');
      td.colSpan = 5;
      td.className = 'empty';
      td.textContent = 'No orders yet.';
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

      const st = document.createElement('td');
      st.append(badge(o.status));

      const total = document.createElement('td');
      total.className = 'num';
      total.textContent = formatKobo(o.totalKobo);

      const when = document.createElement('td');
      when.textContent = new Date(o.createdAt).toLocaleString();

      const act = document.createElement('td');
      const link = document.createElement('a');
      link.className = 'btn btn-sm btn-secondary';
      link.href = `/payment/callback?paymentReference=${encodeURIComponent(o.reference)}`;
      link.textContent = 'Receipt';
      act.append(link);

      tr.append(ref, st, total, when, act);
      body.append(tr);
    }
  } catch (err) {
    document.getElementById('error').textContent = err.message;
    document.getElementById('error').hidden = false;
  }
}

load();

mountFooter();
