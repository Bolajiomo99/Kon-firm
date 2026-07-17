// Point of sale.
//
// Scanning uses the browser's native BarcodeDetector plus getUserMedia — no
// third-party library, no CDN, nothing to break offline or on stage. Support
// is real but uneven (Chrome and Android yes, Safari no), so manual entry is
// always available and is not a second-class path: a real counter needs a way
// to key in a code when a label is scuffed.
import { Cart, formatKobo, toast, apiFetch } from './cart.js';
import { renderThemeToggle } from './theme.js';
import { mountFooter } from './footer.js';
import { currentUser, renderNav } from './auth.js';

const cart = new Cart('konfirm.pos.v1');
const $ = (id) => document.getElementById(id);

const el = {
  reader: $('reader'),
  hint: $('scan-hint'),
  start: $('scan-start'),
  stop: $('scan-stop'),
  scanError: $('scan-error'),
  saleError: $('sale-error'),
  manualForm: $('manual-form'),
  barcode: $('barcode'),
  lines: $('lines'),
  total: $('total'),
  payForm: $('pay-form'),
  payBtn: $('pay-btn'),
  clearBtn: $('clear-btn'),
  demoCodes: $('demo-codes'),
};

let stream = null;
let video = null;
let detector = null;
let scanTimer = null;
let lastCode = '';
let lastCodeAt = 0;

/* ---------- Product lookup ---------- */

async function addByBarcode(code) {
  const barcode = String(code).trim();
  if (!barcode) return;

  try {
    const product = await apiFetch(`/api/products/barcode/${encodeURIComponent(barcode)}`);
    const res = cart.add(product, 1);
    if (!res.ok) {
      toast(res.reason);
      return;
    }
    toast(`${product.name} added`);
    hideError(el.saleError);
  } catch (err) {
    showError(el.saleError, `${barcode}: ${err.message}`);
    toast(`No product for ${barcode}`);
  }
}

/* ---------- Scanner ---------- */

async function startScanner() {
  hideError(el.scanError);

  if (!('BarcodeDetector' in window)) {
    showError(
      el.scanError,
      'This browser has no barcode detector. Chrome or Android will scan; ' +
      'meanwhile you can type the barcode below — it works identically.'
    );
    return;
  }

  if (!navigator.mediaDevices?.getUserMedia) {
    showError(el.scanError, 'Camera access needs a secure context (https or localhost).');
    return;
  }

  try {
    const formats = await window.BarcodeDetector.getSupportedFormats();
    // Keep to retail formats; QR would match unrelated codes lying around.
    const want = ['ean_13', 'ean_8', 'upc_a', 'upc_e', 'code_128', 'code_39'].filter((f) =>
      formats.includes(f)
    );
    detector = new window.BarcodeDetector({ formats: want.length ? want : formats });

    stream = await navigator.mediaDevices.getUserMedia({
      video: { facingMode: 'environment' }, // rear camera on a phone
      audio: false,
    });

    video = document.createElement('video');
    video.setAttribute('playsinline', '');
    video.muted = true;
    video.srcObject = stream;
    await video.play();

    el.reader.innerHTML = '';
    el.reader.append(video);

    el.start.disabled = true;
    el.stop.disabled = false;
    scanLoop();
  } catch (err) {
    const msg =
      err.name === 'NotAllowedError'
        ? 'Camera permission was denied. Type the barcode below instead.'
        : `Could not start the camera: ${err.message}`;
    showError(el.scanError, msg);
    stopScanner();
  }
}

function scanLoop() {
  scanTimer = setInterval(async () => {
    if (!video || video.readyState < 2) return;
    try {
      const codes = await detector.detect(video);
      if (!codes.length) return;

      const value = codes[0].rawValue;
      const now = Date.now();

      // One physical scan fires many frames. Debounce the same code for a
      // moment, or a single item lands in the sale a dozen times.
      if (value === lastCode && now - lastCodeAt < 2500) return;
      lastCode = value;
      lastCodeAt = now;

      addByBarcode(value);
    } catch {
      // A dropped frame is normal; the next tick retries.
    }
  }, 350);
}

function stopScanner() {
  clearInterval(scanTimer);
  scanTimer = null;
  if (stream) {
    stream.getTracks().forEach((t) => t.stop());
    stream = null;
  }
  video = null;
  el.reader.innerHTML = '';
  const p = document.createElement('p');
  p.className = 'scan-hint';
  p.id = 'scan-hint';
  p.textContent = 'Camera is off. Start the scanner, or type a barcode below.';
  el.reader.append(p);
  el.start.disabled = false;
  el.stop.disabled = true;
}

// Releasing the camera on navigation matters: a stray green light after the
// page closes is alarming, and on mobile it drains the battery.
window.addEventListener('pagehide', stopScanner);

/* ---------- Sale ---------- */

function renderSale() {
  el.lines.innerHTML = '';

  if (cart.isEmpty) {
    const li = document.createElement('li');
    li.style.border = '0';
    const s = document.createElement('span');
    s.className = 'scan-hint';
    s.style.padding = '20px 0';
    s.textContent = 'Nothing scanned yet.';
    li.append(s);
    el.lines.append(li);
  } else {
    for (const line of cart.lines) {
      const li = document.createElement('li');

      const n = document.createElement('div');
      n.className = 'n';
      const strong = document.createElement('strong');
      strong.textContent = line.name;
      const span = document.createElement('span');
      span.textContent = `${formatKobo(line.priceKobo)} each`;
      n.append(strong, span);

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

      li.append(n, qty, sub);
      el.lines.append(li);
    }
  }

  el.total.textContent = formatKobo(cart.totalKobo);
  el.payBtn.disabled = cart.isEmpty;
}

el.payForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  hideError(el.saleError);

  const name = $('cust-name').value.trim() || 'Walk-in customer';
  const email = $('cust-email').value.trim();

  if (!email.includes('@')) {
    showError(el.saleError, 'A valid email is required so the customer gets a receipt.');
    return;
  }

  el.payBtn.disabled = true;
  el.payBtn.textContent = 'Contacting Monnify…';

  try {
    const res = await apiFetch('/api/checkout', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        customerName: name,
        customerEmail: email,
        channel: 'pos', // tags the sale as in-store; the admin view splits on this
        items: cart.toItems(),
      }),
    });

    try {
      sessionStorage.setItem('konfirm.lastRef', res.reference);
    } catch { /* non-fatal */ }

    stopScanner();
    cart.clear();
    window.location.href = res.checkoutUrl;
  } catch (err) {
    showError(el.saleError, err.message);
    el.payBtn.disabled = false;
    el.payBtn.textContent = 'Take payment';
  }
});

el.clearBtn.addEventListener('click', () => {
  cart.clear();
  toast('Sale cleared');
});

el.manualForm.addEventListener('submit', (e) => {
  e.preventDefault();
  const code = el.barcode.value;
  el.barcode.value = '';
  addByBarcode(code);
});

/* ---------- Demo barcode shortcuts ---------- */

async function loadDemoCodes() {
  try {
    const products = await apiFetch('/api/products');
    el.demoCodes.innerHTML = '';
    for (const p of products.filter((x) => x.barcode)) {
      const b = document.createElement('button');
      b.className = 'btn btn-sm btn-secondary';
      b.type = 'button';
      b.textContent = `${p.barcode} · ${p.name}`;
      b.addEventListener('click', () => addByBarcode(p.barcode));
      el.demoCodes.append(b);
    }
  } catch {
    el.demoCodes.textContent = 'Could not load demo barcodes.';
  }
}

/* ---------- Helpers ---------- */

function showError(node, msg) {
  node.textContent = msg;
  node.hidden = false;
}
function hideError(node) {
  node.hidden = true;
}

// The counter is staff-only: it looks up the catalogue by barcode and takes
// payment. Neither is a public action.
async function boot() {
  renderThemeToggle(document.getElementById('theme-toggle'));
  const user = await currentUser();
  renderNav(user, document.getElementById('account-nav'));
  if (!user || user.role !== 'admin') {
    window.location.href = '/login?next=/pos';
    return;
  }

  el.start.addEventListener('click', startScanner);
  el.stop.addEventListener('click', stopScanner);
  cart.onChange(renderSale);

  renderSale();
  loadDemoCodes();
}

boot();

mountFooter();
