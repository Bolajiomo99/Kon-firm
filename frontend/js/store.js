// Storefront: carousel, catalogue, bag, and the handoff to Monnify.
import { Cart, formatKobo, toast, apiFetch } from './cart.js';
import { renderThemeToggle } from './theme.js';
import { currentUser, renderNav } from './auth.js';
import { connectLive } from './live.js';
import { buildStateSelect, fetchQuote, renderTotals, useMyLocation } from './checkout.js';
import { mountFooter } from './footer.js';

const cart = new Cart();
const $ = (id) => document.getElementById(id);

let products = [];
let activeCategory = 'Everything';
let sortMode = 'featured';
let user = null;

const el = {
  products: $('products'),
  productsError: $('products-error'),
  categories: $('categories'),
  catTitle: $('cat-title'),
  catCount: $('cat-count'),
  sort: $('sort'),
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
  slides: $('slides'),
  dots: $('slide-dots'),
  guestFields: $('guest-fields'),
  signedInAs: $('signed-in-as'),
  dAddress: $('d-address'),
  dCity: $('d-city'),
  dState: $('d-state'),
  dPhone: $('d-phone'),
  geoBtn: $('geo-btn'),
  geoHint: $('geo-hint'),
  voucher: $('voucher'),
  voucherBtn: $('voucher-btn'),
  voucherHint: $('voucher-hint'),
  totals: {
    subtotal: $('t-subtotal'),
    discountRow: $('t-discount-row'),
    discountLabel: $('t-discount-label'),
    discount: $('t-discount'),
    delivery: $('t-delivery'),
    total: $('cart-total'),
    vat: $('t-vat'),
  },
};

let coords = { lat: null, lng: null };
let appliedVoucher = '';
let quoteTimer = null;

// Re-quote whenever anything that affects the price changes. Debounced: a
// keystroke in the state box should not fire a request per character.
function scheduleQuote() {
  clearTimeout(quoteTimer);
  quoteTimer = setTimeout(refreshQuote, 250);
}

async function refreshQuote() {
  if (cart.isEmpty) {
    renderTotals(
      { subtotalKobo: 0, discountKobo: 0, deliveryFeeKobo: 0, totalKobo: 0, vatKobo: 0, vatRateBp: 750, freeDelivery: false },
      el.totals
    );
    el.totals.delivery.textContent = '—';
    el.totals.vat.textContent = 'Includes VAT';
    return;
  }

  const q = await fetchQuote(cart.toItems(), appliedVoucher, el.dState.value);
  if (!q) return;

  // The server is the authority on whether a code applies. If it rejected one,
  // stop claiming it is applied.
  if (q.voucherError) {
    el.voucherHint.textContent = q.voucherError;
    el.voucherHint.style.color = 'var(--red-600)';
    appliedVoucher = '';
  } else if (q.voucherCode) {
    el.voucherHint.textContent = `${q.voucherCode} applied — ${formatKobo(q.discountKobo)} off.`;
    el.voucherHint.style.color = 'var(--green-600)';
  }

  renderTotals(q, el.totals);
}

/* ---------- Carousel ---------- */

const SLIDES = [
  {
    kicker: 'New season',
    title: 'Built to be worn, not admired',
    body: 'Leather, steel and canvas that age well. Free delivery in Lagos on orders over ₦50,000.',
    cta: 'Shop fashion',
    category: 'Fashion',
    img: 'https://images.unsplash.com/photo-1491553895911-0055eca6402d?w=1600&q=80&fm=jpg&fit=crop',
  },
  {
    kicker: 'Gadgets',
    title: 'Sound that shuts the road out',
    body: 'Noise-cancelling headphones, mechanical keyboards and cameras with real dials.',
    cta: 'Shop gadgets',
    category: 'Gadgets',
    img: 'https://images.unsplash.com/photo-1505740420928-5e560c06d30e?w=1600&q=80&fm=jpg&fit=crop',
  },
  {
    kicker: 'Confirmed by Monnify',
    title: 'Pay, and know it landed',
    body: 'Your order is only released once Monnify verifies the payment. No guesswork, no chasing receipts.',
    cta: 'Start shopping',
    category: 'Everything',
    img: 'https://images.unsplash.com/photo-1523275335684-37898b6baf30?w=1600&q=80&fm=jpg&fit=crop',
  },
];

let slideIndex = 0;
let slideTimer = null;

function buildCarousel() {
  el.slides.innerHTML = '';
  el.dots.innerHTML = '';

  SLIDES.forEach((s, i) => {
    const slide = document.createElement('div');
    slide.className = 'slide';
    slide.setAttribute('aria-roledescription', 'slide');
    slide.setAttribute('aria-label', `${i + 1} of ${SLIDES.length}`);

    const media = document.createElement('div');
    media.className = 'slide-media';
    const img = document.createElement('img');
    img.src = s.img;
    img.alt = '';
    // The first slide is the largest thing above the fold, so it is not lazy:
    // deferring it would leave the hero grey while it fetches.
    img.loading = i === 0 ? 'eager' : 'lazy';
    img.fetchPriority = i === 0 ? 'high' : 'auto';
    media.append(img);

    const body = document.createElement('div');
    body.className = 'slide-body';
    const copy = document.createElement('div');
    copy.className = 'slide-copy';

    const kicker = document.createElement('span');
    kicker.className = 'kicker';
    kicker.textContent = s.kicker;

    const h2 = document.createElement('h2');
    h2.textContent = s.title;

    const p = document.createElement('p');
    p.textContent = s.body;

    const btn = document.createElement('button');
    btn.className = 'btn btn-lg';
    btn.type = 'button';
    btn.textContent = s.cta;
    btn.addEventListener('click', () => {
      setCategory(s.category);
      document.getElementById('catalogue').scrollIntoView({ behavior: 'smooth', block: 'start' });
    });

    copy.append(kicker, h2, p, btn);
    body.append(copy);
    slide.append(media, body);
    el.slides.append(slide);

    const dot = document.createElement('button');
    dot.type = 'button';
    dot.setAttribute('aria-label', `Go to slide ${i + 1}`);
    dot.addEventListener('click', () => goToSlide(i, true));
    el.dots.append(dot);
  });

  goToSlide(0);
  startAutoplay();
}

function goToSlide(i, manual = false) {
  slideIndex = (i + SLIDES.length) % SLIDES.length;
  el.slides.style.transform = `translateX(-${slideIndex * 100}%)`;
  [...el.dots.children].forEach((d, n) =>
    d.setAttribute('aria-current', String(n === slideIndex))
  );
  // A tap means the visitor is driving; do not yank the slide away from them.
  if (manual) startAutoplay();
}

function startAutoplay() {
  clearInterval(slideTimer);
  // Respect a reduced-motion preference: auto-advancing a carousel is exactly
  // the kind of motion that setting exists to stop.
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
  slideTimer = setInterval(() => goToSlide(slideIndex + 1), 6000);
}

$('slide-prev').addEventListener('click', () => goToSlide(slideIndex - 1, true));
$('slide-next').addEventListener('click', () => goToSlide(slideIndex + 1, true));
$('carousel').addEventListener('mouseenter', () => clearInterval(slideTimer));
$('carousel').addEventListener('mouseleave', startAutoplay);
// Autoplay in a hidden tab burns battery for nobody's benefit.
document.addEventListener('visibilitychange', () =>
  document.hidden ? clearInterval(slideTimer) : startAutoplay()
);

/* ---------- Categories ---------- */

function buildCategories() {
  const cats = ['Everything', ...new Set(products.map((p) => p.category))];
  el.categories.innerHTML = '';

  for (const c of cats) {
    const li = document.createElement('li');
    const b = document.createElement('button');
    b.type = 'button';
    b.textContent = c;
    b.setAttribute('aria-selected', String(c === activeCategory));
    b.addEventListener('click', () => setCategory(c));
    li.append(b);
    el.categories.append(li);
  }
}

function setCategory(c) {
  activeCategory = c;
  buildCategories();
  renderProducts();
}

/* ---------- Catalogue ---------- */

async function loadProducts() {
  try {
    products = await apiFetch('/api/products');
    buildCategories();
    renderProducts();
  } catch (err) {
    el.products.innerHTML = '';
    el.products.setAttribute('aria-busy', 'false');
    el.productsError.textContent = `Could not load the catalogue: ${err.message}`;
    el.productsError.hidden = false;
  }
}

function visibleProducts() {
  let list = activeCategory === 'Everything'
    ? [...products]
    : products.filter((p) => p.category === activeCategory);

  switch (sortMode) {
    case 'price-asc': list.sort((a, b) => a.priceKobo - b.priceKobo); break;
    case 'price-desc': list.sort((a, b) => b.priceKobo - a.priceKobo); break;
    case 'rating': list.sort((a, b) => (b.rating || 0) - (a.rating || 0)); break;
    default:
      // Featured: new first, then discounted, then best rated.
      list.sort((a, b) =>
        (b.isNew - a.isNew) ||
        ((b.compareAtKobo ? 1 : 0) - (a.compareAtKobo ? 1 : 0)) ||
        ((b.rating || 0) - (a.rating || 0))
      );
  }
  return list;
}

function stars(rating) {
  const full = Math.round(rating || 0);
  return '★'.repeat(full) + '☆'.repeat(5 - full);
}

function renderProducts() {
  el.products.setAttribute('aria-busy', 'false');
  const list = visibleProducts();

  el.catTitle.textContent = activeCategory;
  el.catCount.textContent =
    list.length === 1 ? '1 product' : `${list.length} products`;

  el.products.innerHTML = '';
  if (!list.length) {
    const p = document.createElement('p');
    p.className = 'empty';
    p.textContent = 'Nothing in this category yet.';
    el.products.append(p);
    return;
  }

  for (const p of list) {
    const card = document.createElement('article');
    card.className = 'card';

    const media = document.createElement('div');
    media.className = 'card-media';

    const img = document.createElement('img');
    img.src = p.imageUrl;
    img.alt = p.name;
    img.loading = 'lazy';
    img.decoding = 'async';
    // The photos are on a third-party CDN. If it is slow, blocked, or down,
    // the card must still be a card — not a broken-image icon.
    img.addEventListener('error', () => {
      img.remove();
      media.style.display = 'grid';
      media.style.placeItems = 'center';
      const fb = document.createElement('span');
      fb.textContent = p.category;
      fb.style.cssText = 'font-size:.72rem;letter-spacing:.1em;text-transform:uppercase;color:var(--text-muted)';
      media.append(fb);
    }, { once: true });
    media.append(img);

    const badges = document.createElement('div');
    badges.className = 'card-badges';
    if (p.compareAtKobo) {
      const off = Math.round((1 - p.priceKobo / p.compareAtKobo) * 100);
      const t = document.createElement('span');
      t.className = 'tag tag-sale';
      t.textContent = `-${off}%`;
      badges.append(t);
    }
    if (p.isNew) {
      const t = document.createElement('span');
      t.className = 'tag tag-new';
      t.textContent = 'New';
      badges.append(t);
    }
    if (p.stock > 0 && p.stock <= 5) {
      const t = document.createElement('span');
      t.className = 'tag tag-low';
      t.textContent = `Only ${p.stock}`;
      badges.append(t);
    }
    if (badges.children.length) media.append(badges);

    const body = document.createElement('div');
    body.className = 'card-body';

    const cat = document.createElement('span');
    cat.className = 'card-cat';
    cat.textContent = p.category;

    const h3 = document.createElement('h3');
    h3.textContent = p.name;

    const rate = document.createElement('div');
    rate.className = 'rating';
    if (p.rating) {
      const s = document.createElement('span');
      s.className = 'stars';
      s.setAttribute('aria-hidden', 'true');
      s.textContent = stars(p.rating);
      const n = document.createElement('span');
      n.textContent = `${p.rating.toFixed(1)} (${p.reviewCount.toLocaleString('en-NG')})`;
      rate.append(s, n);
      rate.setAttribute('aria-label', `Rated ${p.rating} out of 5 from ${p.reviewCount} reviews`);
    }

    const priceRow = document.createElement('div');
    priceRow.className = 'price-row';
    const price = document.createElement('span');
    price.className = 'price';
    price.textContent = formatKobo(p.priceKobo);
    priceRow.append(price);
    if (p.compareAtKobo) {
      const was = document.createElement('span');
      was.className = 'price-was';
      was.textContent = formatKobo(p.compareAtKobo);
      priceRow.append(was);
    }

    const stock = document.createElement('span');
    stock.className = 'stock-line';
    if (p.stock <= 0) {
      stock.textContent = 'Out of stock';
      stock.classList.add('out');
    } else if (p.stock <= 5) {
      stock.textContent = `Only ${p.stock} left`;
      stock.classList.add('low');
    } else {
      stock.textContent = 'In stock';
    }

    const btn = document.createElement('button');
    btn.className = 'btn btn-block btn-sm';
    btn.type = 'button';
    btn.textContent = p.stock > 0 ? 'Add to bag' : 'Out of stock';
    btn.disabled = p.stock <= 0;
    btn.style.marginTop = '8px';
    btn.addEventListener('click', () => {
      const res = cart.add(p, 1);
      if (!res.ok) return toast(res.reason);
      toast(`${p.name} added to bag`);
      openCart();
    });

    body.append(cat, h3, rate, priceRow, stock, btn);
    card.append(media, body);
    el.products.append(card);
  }
}

el.sort.addEventListener('change', () => {
  sortMode = el.sort.value;
  renderProducts();
});

/* ---------- Bag ---------- */

let lastFocused = null;

function openCart() {
  lastFocused = document.activeElement;
  el.cart.dataset.open = 'true';
  el.cart.setAttribute('aria-hidden', 'false');
  el.backdrop.hidden = false;
  requestAnimationFrame(() => { el.backdrop.dataset.open = 'true'; });
  el.closeBtn.focus();
  document.addEventListener('keydown', onEsc);
}

function closeCart() {
  el.cart.dataset.open = 'false';
  el.cart.setAttribute('aria-hidden', 'true');
  el.backdrop.dataset.open = 'false';
  setTimeout(() => { el.backdrop.hidden = true; }, 250);
  document.removeEventListener('keydown', onEsc);
  lastFocused?.focus();
}

function onEsc(e) {
  if (e.key === 'Escape') closeCart();
}

function renderCart() {
  el.cartCount.textContent = String(cart.count);
  el.cartCount.hidden = cart.count === 0;
  el.checkoutBtn.disabled = cart.isEmpty;
  scheduleQuote();

  el.cartItems.innerHTML = '';
  if (cart.isEmpty) {
    const p = document.createElement('p');
    p.className = 'empty';
    p.textContent = 'Your bag is empty.';
    el.cartItems.append(p);
    return;
  }

  for (const line of cart.lines) {
    const row = document.createElement('div');
    row.className = 'cart-row';

    if (line.imageUrl) {
      const th = document.createElement('img');
      th.className = 'cart-thumb';
      th.src = line.imageUrl;
      th.alt = '';
      th.loading = 'lazy';
      th.addEventListener('error', () => th.remove(), { once: true });
      row.append(th);
    }

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
    minus.setAttribute('aria-label', `Remove one ${line.name}`);
    minus.addEventListener('click', () => cart.setQuantity(line.productId, line.quantity - 1));
    const out = document.createElement('output');
    out.textContent = String(line.quantity);
    const plus = document.createElement('button');
    plus.type = 'button';
    plus.textContent = '+';
    plus.setAttribute('aria-label', `Add one ${line.name}`);
    plus.disabled = line.quantity >= line.stock;
    plus.addEventListener('click', () => cart.setQuantity(line.productId, line.quantity + 1));
    qty.append(minus, out, plus);

    const sub = document.createElement('span');
    sub.className = 'price';
    sub.style.fontSize = '.92rem';
    sub.textContent = formatKobo(line.priceKobo * line.quantity);

    row.append(info, qty, sub);
    el.cartItems.append(row);
  }
}

/* ---------- Checkout ---------- */

el.form.addEventListener('submit', async (e) => {
  e.preventDefault();
  el.checkoutError.hidden = true;

  const name = user ? user.name : $('name').value.trim();
  const email = user ? (user.email || $('email').value.trim()) : $('email').value.trim();

  if (!name) return showCheckoutError('Please enter your name.');
  if (!email.includes('@')) return showCheckoutError('Please enter a valid email address.');
  if (cart.isEmpty) return showCheckoutError('Your bag is empty.');
  if (!el.dAddress.value.trim()) return showCheckoutError('Please enter a delivery address.');
  if (!el.dState.value) return showCheckoutError('Please choose your state.');

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
        voucherCode: appliedVoucher,
        deliveryPhone: el.dPhone.value.trim(),
        deliveryAddress: el.dAddress.value.trim(),
        deliveryCity: el.dCity.value.trim(),
        deliveryState: el.dState.value,
        deliveryLat: coords.lat,
        deliveryLng: coords.lng,
      }),
    });

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

function safeSession(key) {
  try {
    return sessionStorage.getItem(key);
  } catch {
    return null;
  }
}

/* ---------- Boot ---------- */

el.openBtn.addEventListener('click', openCart);
el.closeBtn.addEventListener('click', closeCart);
el.backdrop.addEventListener('click', closeCart);
cart.onChange(renderCart);

async function boot() {
  mountFooter();
  renderThemeToggle(document.getElementById('theme-toggle'));
  user = await currentUser();
  renderNav(user, $('account-nav'));

  buildStateSelect(el.dState);

  // A signed-in shopper has already told us who they are; asking again is
  // friction for no information.
  if (user) {
    el.guestFields.hidden = true;
    el.signedInAs.hidden = false;
    el.signedInAs.innerHTML = '';
    const av = document.createElement('span');
    av.className = 'avatar';
    av.textContent = (user.name.split(' ').map(p => p[0]).slice(0,2).join('') || '?').toUpperCase();
    const info = document.createElement('div');
    const nm = document.createElement('strong');
    nm.textContent = user.name;
    const sub = document.createElement('span');
    sub.textContent = user.email || user.phonePretty || '';
    info.append(nm, sub);
    el.signedInAs.append(av, info);
    if (user.phone && !el.dPhone.value) el.dPhone.value = user.phonePretty || user.phone;
  } else {
    el.guestFields.hidden = false;
    el.signedInAs.hidden = true;
  }

  el.dState.addEventListener('change', scheduleQuote);
  el.geoBtn.addEventListener('click', () =>
    useMyLocation(el.geoHint, (lat, lng) => { coords = { lat, lng }; })
  );
  el.voucherBtn.addEventListener('click', () => {
    appliedVoucher = el.voucher.value.trim().toUpperCase();
    if (!appliedVoucher) {
      el.voucherHint.textContent = 'Enter a code first.';
      el.voucherHint.style.color = '';
      return;
    }
    refreshQuote();
  });
  el.voucher.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { e.preventDefault(); el.voucherBtn.click(); }
  });

  buildCarousel();
  renderCart();
  await loadProducts();

  // Stock is shared with the shop counter, so it moves under a shopper's feet.
  const lastRef = safeSession('konfirm.lastRef');
  if (lastRef) {
    connectLive({
      order: lastRef,
      onEvent: (ev) => {
        if (ev.type === 'order.paid') {
          toast('Your payment was confirmed');
          loadProducts();
        }
      },
    });
  }
}

boot();
