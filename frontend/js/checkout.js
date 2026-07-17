// Checkout: delivery, voucher, and the live price breakdown.
//
// Every figure shown here is computed by the server. The browser never adds up
// a total, applies a discount, or works out VAT — it asks /api/quote and
// renders the answer. That is why the number in the bag is always the number
// Monnify charges: there is only one implementation of the arithmetic.
import { formatKobo, apiFetch } from './cart.js';

// Nigeria's 36 states plus the FCT.
export const NG_STATES = [
  'Lagos', 'Abia', 'Adamawa', 'Akwa Ibom', 'Anambra', 'Bauchi', 'Bayelsa',
  'Benue', 'Borno', 'Cross River', 'Delta', 'Ebonyi', 'Edo', 'Ekiti', 'Enugu',
  'FCT — Abuja', 'Gombe', 'Imo', 'Jigawa', 'Kaduna', 'Kano', 'Katsina',
  'Kebbi', 'Kogi', 'Kwara', 'Nasarawa', 'Niger', 'Ogun', 'Ondo', 'Osun',
  'Oyo', 'Plateau', 'Rivers', 'Sokoto', 'Taraba', 'Yobe', 'Zamfara',
];

export function buildStateSelect(select) {
  if (!select) return;
  select.innerHTML = '';
  const none = document.createElement('option');
  none.value = '';
  none.textContent = 'Choose a state…';
  select.append(none);
  for (const s of NG_STATES) {
    const o = document.createElement('option');
    // Lagos is the value the server checks for free delivery, so the FCT's
    // display name must not become its value.
    o.value = s.startsWith('FCT') ? 'FCT' : s;
    o.textContent = s;
    select.append(o);
  }
  select.value = 'Lagos'; // most orders, and the only free-delivery state
}

/** Asks the server what the basket costs. Returns null on failure. */
export async function fetchQuote(items, voucherCode, state) {
  if (!items.length) return null;
  try {
    return await apiFetch('/api/quote', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ items, voucherCode: voucherCode || '', state: state || '' }),
    });
  } catch {
    return null;
  }
}

/** Renders a server quote into the totals panel. */
export function renderTotals(q, els) {
  if (!q) return;

  els.subtotal.textContent = formatKobo(q.subtotalKobo);

  if (q.discountKobo > 0) {
    els.discountRow.hidden = false;
    els.discountLabel.textContent = q.voucherCode ? `Discount (${q.voucherCode})` : 'Discount';
    els.discount.textContent = '−' + formatKobo(q.discountKobo);
    els.discount.className = 'off';
  } else {
    els.discountRow.hidden = true;
  }

  if (q.freeDelivery) {
    els.delivery.textContent = 'FREE';
    els.delivery.className = 'free';
  } else {
    els.delivery.textContent = formatKobo(q.deliveryFeeKobo);
    els.delivery.className = '';
  }

  els.total.textContent = formatKobo(q.totalKobo);

  const pct = (q.vatRateBp / 100).toFixed(1).replace(/\.0$/, '');
  els.vat.textContent = `Includes VAT of ${formatKobo(q.vatKobo)} at ${pct}%`;
}

/**
 * Fills the address from the device's location.
 *
 * Uses the browser's own Geolocation API rather than a maps SDK: no API key,
 * no billing account, no third-party script for the CSP to allow, and nothing
 * that can fail on stage. It captures coordinates for the dispatch rider —
 * it does not replace the written address, which a rider still needs.
 */
export function useMyLocation(hintEl, onCoords) {
  if (!navigator.geolocation) {
    hintEl.textContent = 'This browser cannot share a location. Please type the address.';
    return;
  }
  hintEl.textContent = 'Finding you…';

  navigator.geolocation.getCurrentPosition(
    (pos) => {
      const { latitude, longitude, accuracy } = pos.coords;
      onCoords(latitude, longitude);
      hintEl.textContent =
        `📍 Location saved for the rider (±${Math.round(accuracy)}m). Please still type your address.`;
    },
    (err) => {
      hintEl.textContent =
        err.code === err.PERMISSION_DENIED
          ? 'Location permission denied — please type your address.'
          : 'Could not get your location — please type your address.';
    },
    { enableHighAccuracy: true, timeout: 10000, maximumAge: 60000 }
  );
}
