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
  if (els.totalPinned) els.totalPinned.textContent = formatKobo(q.totalKobo);

  const pct = (q.vatRateBp / 100).toFixed(1).replace(/\.0$/, '');
  els.vat.textContent = `Includes VAT of ${formatKobo(q.vatKobo)} at ${pct}%`;
}

/**
 * Fills the delivery address from the device's location.
 *
 * The browser's own Geolocation API gives coordinates; our server turns those
 * into a street, city and state. The lookup is proxied rather than called from
 * here because `connect-src 'self'` blocks a direct call, and widening the CSP
 * to allow a geocoder would open the one door keeping third parties out of the
 * page.
 *
 * The fields stay editable afterwards. Reverse geocoding lands on a street, not
 * a gate — a rider still needs "third gate, blue building", and no geocoder
 * knows that.
 */
export function useMyLocation(hintEl, fields, onCoords) {
  if (!navigator.geolocation) {
    hintEl.textContent = 'This browser cannot share a location. Please type your address.';
    return;
  }
  hintEl.textContent = 'Finding you…';
  hintEl.style.color = '';

  navigator.geolocation.getCurrentPosition(
    async (pos) => {
      const { latitude, longitude, accuracy } = pos.coords;
      onCoords(latitude, longitude);
      hintEl.textContent = `📍 Found you (±${Math.round(accuracy)}m). Looking up the address…`;

      try {
        const res = await apiFetch(
          `/api/geocode/reverse?lat=${latitude.toFixed(6)}&lng=${longitude.toFixed(6)}`
        );

        // Never overwrite something the shopper already typed — they know
        // their address better than a geocoder does.
        if (res.address && !fields.address.value.trim()) fields.address.value = res.address;
        if (res.city && !fields.city.value.trim()) fields.city.value = res.city;
        if (res.state) {
          const opt = [...fields.state.options].find((o) => o.value === res.state);
          if (opt) fields.state.value = res.state;
        }
        fields.state.dispatchEvent(new Event('change')); // delivery fee may change

        hintEl.textContent = res.address
          ? `📍 ${res.address}${res.city ? ', ' + res.city : ''} — please check and add your landmark.`
          : '📍 Location saved. Please type your street address.';
        hintEl.style.color = 'var(--green-600)';
      } catch {
        hintEl.textContent =
          '📍 Location saved for the rider, but the address lookup failed — please type it.';
      }
    },
    (err) => {
      hintEl.textContent =
        err.code === err.PERMISSION_DENIED
          ? 'Location permission denied — please type your address.'
          : 'Could not get your location — please type your address.';
      hintEl.style.color = 'var(--red-600)';
    },
    { enableHighAccuracy: true, timeout: 10000, maximumAge: 60000 }
  );
}
