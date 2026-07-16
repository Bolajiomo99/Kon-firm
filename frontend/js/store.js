// Storefront: catalogue, cart panel, and checkout handoff to Monnify.
import { Cart, formatKobo, toast, apiFetch } from './cart.js';

const cart = new Cart();
let products = [];

const $ = (id) => document.getElementById(id);

const el = {
  products: $('products'),
  productsError: $('products-error'),
  cart: $('cart'),
  cartItems: $('cart-items'),
  cartTotal: $('cart-total'),
  cartCount: $('cart-count'),
  backdrop: $('backdrop'),
  openBtn: $('cart-open'),
  closeBtn: $('cart-close'),
  form: $('checkout-form'),
  checkoutBtn: $('checkout-btn'),
  checkoutError: $('checkout-error'),
};

/* ---------- Catalogue ---------- */

async function loadProducts() {
  try {
    products = await apiFetch('/api/products');
    renderProducts();
  } catch (err) {
    el.products.innerHTML = '';
    el.products.setAttribute('aria-busy', 'false');
    el.productsError.textContent = `Could not load the catalogue: ${err.message}`;
    el.productsError.hidden = false;
  }
}

function stockLabel(stock) {
  if (stock <= 0) return { text: 'Out of stock', cls: 'out' };
  if (stock <= 5) return { text: `Only ${stock} left`, cls: 'low' };
  return { text: `${stock} in stock`, cls: '' };
}

function renderProducts() {
  el.products.setAttribute('aria-busy', 'false');

  if (!products.length) {
    el.products.innerHTML = '<p class="empty">No products yet.</p>';
    return;
  }

  el.products.innerHTML = '';
  for (const p of products) {
    const s = stockLabel(p.stock);
    const card = document.createElement('article');
    card.className = 'card';

    // Built with DOM APIs rather than innerHTML: product copy comes from the
    // database, and textContent cannot be coerced into markup.
    const media = document.createElement('div');
    media.className = 'card-media';
    const img = document.createElement('img');
    img.src = p.imageUrl || '/img/placeholder.svg';
    img.alt = p.name;
    img.loading = 'lazy';
    // A missing image must degrade to the placeholder, not a broken icon.
    img.addEventListener('error', () => { img.src = '/img/placeholder.svg'; }, { once: true });
    media.append(img);

    const body = document.createElement('div');
    body.className = 'card-body';

    const h3 = document.createElement('h3');
    h3.textContent = p.name;

    const desc = document.createElement('p');
    desc.className = 'desc';
    desc.textContent = p.description;

    const priceRow = document.createElement('div');
    priceRow.className = 'price-row';
    const price = document.createElement('span');
    price.className = 'price';
    price.textContent = formatKobo(p.priceKobo);
    const stock = document.createElement('span');
    stock.className = `stock ${s.cls}`.trim();
    stock.textContent = s.text;
    priceRow.append(price, stock);

    const btn = document.createElement('button');
    btn.className = 'btn btn-block';
    btn.textContent = p.stock > 0 ? 'Add to cart' : 'Out of stock';
    btn.disabled = p.stock <= 0;
    btn.addEventListener('click', () => {
      const res = cart.add(p, 1);
      if (!res.ok) return toast(res.reason);
      toast(`${p.name} added`);
    });

    body.append(h3, desc, priceRow, btn);
    card.append(media, body);
    el.products.append(card);
  }
}

/* ---------- Cart panel ---------- */

let lastFocused = null;

function openCart() {
  lastFocused = document.activeElement;
  el.cart.dataset.open = 'true';
  el.cart.setAttribute('aria-hidden', 'false');
  el.backdrop.hidden = false;
  el.backdrop.dataset.open = 'true';
  el.closeBtn.focus();
  document.addEventListener('keydown', onEsc);
}

function closeCart() {
  el.cart.dataset.open = 'false';
  el.cart.setAttribute('aria-hidden', 'true');
  el.backdrop.dataset.open = 'false';
  // Wait for the transition before hiding, so it does not snap away.
  setTimeout(() => { el.backdrop.hidden = true; }, 220);
  document.removeEventListener('keydown', onEsc);
  if (lastFocused) lastFocused.focus();
}

function onEsc(e) {
  if (e.key === 'Escape') closeCart();
}

function renderCart() {
  el.cartCount.textContent = String(cart.count);
  el.cartCount.hidden = cart.count === 0;
  el.cartTotal.textContent = formatKobo(cart.totalKobo);
  el.checkoutBtn.disabled = cart.isEmpty;

  el.cartItems.innerHTML = '';
  if (cart.isEmpty) {
    const p = document.createElement('p');
    p.className = 'empty';
    p.textContent = 'Your cart is empty.';
    el.cartItems.append(p);
    return;
  }

  for (const line of cart.lines) {
    const row = document.createElement('div');
    row.className = 'cart-row';

    const info = document.createElement('div');
    info.className = 'info';
    const strong = document.createElement('strong');
    strong.textContent = line.name;
    const span = document.createElement('span');
    span.textContent = `${formatKobo(line.priceKobo)} each`;
    info.append(strong, span);

    const qty = document.createElement('div');
    qty.className = 'qty';

    const minus = document.createElement('button');
    minus.type = 'button';
    minus.textContent = '−';
    minus.setAttribute('aria-label', `Decrease ${line.name}`);
    minus.addEventListener('click', () => cart.setQuantity(line.productId, line.quantity - 1));

    const out = document.createElement('output');
    out.textContent = String(line.quantity);

    const plus = document.createElement('button');
    plus.type = 'button';
    plus.textContent = '+';
    plus.setAttribute('aria-label', `Increase ${line.name}`);
    plus.disabled = line.quantity >= line.stock;
    plus.addEventListener('click', () => cart.setQuantity(line.productId, line.quantity + 1));

    qty.append(minus, out, plus);

    const sub = document.createElement('span');
    sub.className = 'price';
    sub.style.fontSize = '.95rem';
    sub.textContent = formatKobo(line.priceKobo * line.quantity);

    row.append(info, qty, sub);
    el.cartItems.append(row);
  }
}

/* ---------- Checkout ---------- */

el.form.addEventListener('submit', async (e) => {
  e.preventDefault();
  el.checkoutError.hidden = true;

  const name = $('name').value.trim();
  const email = $('email').value.trim();

  if (!name) return showCheckoutError('Please enter your name.');
  if (!email.includes('@')) return showCheckoutError('Please enter a valid email address.');
  if (cart.isEmpty) return showCheckoutError('Your cart is empty.');

  el.checkoutBtn.disabled = true;
  el.checkoutBtn.textContent = 'Contacting Monnify…';

  try {
    const res = await apiFetch('/api/checkout', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        customerName: name,
        customerEmail: email,
        channel: 'online',
        items: cart.toItems(),
      }),
    });

    // Remember the reference so the callback page can look the order up even
    // if Monnify's redirect drops the query string.
    try {
      sessionStorage.setItem('konfirm.lastRef', res.reference);
    } catch { /* non-fatal */ }

    cart.clear();
    window.location.href = res.checkoutUrl;
  } catch (err) {
    showCheckoutError(err.message);
    el.checkoutBtn.disabled = false;
    el.checkoutBtn.textContent = 'Pay with Monnify';
  }
});

function showCheckoutError(msg) {
  el.checkoutError.textContent = msg;
  el.checkoutError.hidden = false;
}

/* ---------- Wire up ---------- */

el.openBtn.addEventListener('click', openCart);
el.closeBtn.addEventListener('click', closeCart);
el.backdrop.addEventListener('click', closeCart);
cart.onChange(renderCart);

renderCart();
loadProducts();
