// Product management for the admin dashboard.
import { formatKobo, apiFetch, toast } from './cart.js';

const $ = (id) => document.getElementById(id);
let editingId = null;

/** Naira string -> kobo. Money never goes through parseFloat arithmetic. */
function nairaToKobo(v) {
  const s = String(v ?? '').replace(/[₦,\s]/g, '').trim();
  if (!s) return 0;
  const [whole, frac = ''] = s.split('.');
  const kobo = (frac + '00').slice(0, 2);
  return (parseInt(whole || '0', 10) * 100) + parseInt(kobo, 10);
}

function koboToNairaInput(k) {
  if (k == null) return '';
  return (k / 100).toFixed(2);
}

export function openProductForm(product) {
  editingId = product?.id ?? null;
  $('pf-title').textContent = product ? `Edit ${product.name}` : 'Add a product';
  $('pf-sku').value = product?.sku ?? '';
  $('pf-name').value = product?.name ?? '';
  $('pf-category').value = product?.category ?? 'Fashion';
  $('pf-desc').value = product?.description ?? '';
  $('pf-price').value = product ? koboToNairaInput(product.priceKobo) : '';
  $('pf-compare').value = product?.compareAtKobo ? koboToNairaInput(product.compareAtKobo) : '';
  $('pf-stock').value = product?.stock ?? 0;
  $('pf-barcode').value = product?.barcode ?? '';
  $('pf-image').value = product?.imageUrl ?? '';
  $('pf-new').checked = product?.isNew ?? false;
  $('pf-active').checked = product?.active ?? true;
  $('pf-error').hidden = true;
  updatePreview();
  $('product-modal').hidden = false;
  $('pf-name').focus();
}

export function closeProductForm() {
  $('product-modal').hidden = true;
  editingId = null;
}

// Live image preview. The point is to see a broken URL here rather than
// discover it as a blank card on the storefront.
function updatePreview() {
  const url = $('pf-image').value.trim();
  const box = $('pf-preview');
  box.innerHTML = '';
  if (!url) {
    box.textContent = 'No image';
    return;
  }
  const img = document.createElement('img');
  img.src = url;
  img.alt = '';
  img.addEventListener('error', () => {
    box.innerHTML = '';
    box.textContent = '⚠️ That image will not load';
    box.style.color = 'var(--red-600)';
  }, { once: true });
  img.addEventListener('load', () => { box.style.color = ''; }, { once: true });
  box.append(img);
}

async function save(e) {
  e.preventDefault();
  const err = $('pf-error');
  err.hidden = true;

  const compare = nairaToKobo($('pf-compare').value);
  const payload = {
    sku: $('pf-sku').value.trim(),
    name: $('pf-name').value.trim(),
    category: $('pf-category').value,
    description: $('pf-desc').value.trim(),
    priceKobo: nairaToKobo($('pf-price').value),
    // null, not 0: "not on sale" and "free" are different things.
    compareAtKobo: compare > 0 ? compare : null,
    stock: parseInt($('pf-stock').value || '0', 10),
    barcode: $('pf-barcode').value.trim(),
    imageUrl: $('pf-image').value.trim(),
    isNew: $('pf-new').checked,
    active: $('pf-active').checked,
  };

  try {
    if (editingId) {
      await apiFetch(`/api/admin/products/${editingId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      toast(`${payload.name} updated`);
    } else {
      await apiFetch('/api/admin/products', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      toast(`${payload.name} added`);
    }
    closeProductForm();
    document.dispatchEvent(new CustomEvent('products:changed'));
  } catch (e2) {
    err.textContent = e2.message;
    err.hidden = false;
  }
}

export async function removeProduct(p) {
  if (!window.confirm(`Remove "${p.name}"?\n\nIf it has orders against it, it will be hidden from the shop instead of deleted — deleting it would erase lines from past receipts.`)) {
    return;
  }
  try {
    const res = await apiFetch(`/api/admin/products/${p.id}`, { method: 'DELETE' });
    toast(res.status === 'deactivated' ? `${p.name} hidden from the shop` : `${p.name} deleted`);
    document.dispatchEvent(new CustomEvent('products:changed'));
  } catch (e) {
    toast(`Could not remove: ${e.message}`);
  }
}

export function renderProductsTable(products, tbody) {
  tbody.innerHTML = '';
  if (!products.length) {
    const tr = document.createElement('tr');
    const td = document.createElement('td');
    td.colSpan = 7;
    td.className = 'empty';
    td.textContent = 'No products yet.';
    tr.append(td);
    tbody.append(tr);
    return;
  }

  for (const p of products) {
    const tr = document.createElement('tr');
    if (!p.active) tr.style.opacity = '0.5';

    const imgTd = document.createElement('td');
    if (p.imageUrl) {
      const im = document.createElement('img');
      im.src = p.imageUrl;
      im.alt = '';
      im.className = 'admin-thumb';
      im.loading = 'lazy';
      im.addEventListener('error', () => im.remove(), { once: true });
      imgTd.append(im);
    }

    const nameTd = document.createElement('td');
    const nm = document.createElement('strong');
    nm.textContent = p.name;
    nm.style.display = 'block';
    const sku = document.createElement('code');
    sku.className = 'ref';
    sku.textContent = p.sku + (p.active ? '' : ' · hidden');
    nameTd.append(nm, sku);

    const catTd = document.createElement('td');
    catTd.textContent = p.category;

    const priceTd = document.createElement('td');
    priceTd.className = 'num';
    priceTd.textContent = formatKobo(p.priceKobo);

    const stockTd = document.createElement('td');
    stockTd.className = 'num';
    const st = document.createElement('span');
    st.textContent = String(p.stock);
    if (p.stock <= 0) st.className = 'stock-line out';
    else if (p.stock <= 5) st.className = 'stock-line low';
    stockTd.append(st);

    const flagTd = document.createElement('td');
    if (p.isNew) {
      const b = document.createElement('span');
      b.className = 'tag tag-new';
      b.textContent = 'New';
      flagTd.append(b);
    }
    if (p.compareAtKobo) {
      const b = document.createElement('span');
      b.className = 'tag tag-sale';
      b.textContent = 'Sale';
      b.style.marginLeft = '4px';
      flagTd.append(b);
    }

    const actTd = document.createElement('td');
    actTd.style.whiteSpace = 'nowrap';
    const edit = document.createElement('button');
    edit.className = 'btn btn-sm btn-secondary';
    edit.type = 'button';
    edit.textContent = 'Edit';
    edit.addEventListener('click', () => openProductForm(p));
    const del = document.createElement('button');
    del.className = 'btn btn-sm btn-secondary';
    del.type = 'button';
    del.textContent = 'Remove';
    del.style.marginLeft = '6px';
    del.style.color = 'var(--red-600)';
    del.addEventListener('click', () => removeProduct(p));
    actTd.append(edit, del);

    tr.append(imgTd, nameTd, catTd, priceTd, stockTd, flagTd, actTd);
    tbody.append(tr);
  }
}

export function initProductForm() {
  $('product-form').addEventListener('submit', save);
  $('pf-cancel').addEventListener('click', closeProductForm);
  $('pf-image').addEventListener('input', updatePreview);
  $('product-modal').addEventListener('click', (e) => {
    if (e.target.id === 'product-modal') closeProductForm();
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !$('product-modal').hidden) closeProductForm();
  });
}
