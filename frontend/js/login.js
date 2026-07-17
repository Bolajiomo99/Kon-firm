import { apiFetch, toast } from './cart.js';
import { mountFooter } from './footer.js';

const form = document.getElementById('form');
const errorBox = document.getElementById('error');
const submit = document.getElementById('submit');

// Only same-origin paths are honoured as a redirect target. Accepting an
// absolute URL here would make this an open redirect: a link to
// /login?next=https://evil.example would bounce a freshly-signed-in user
// straight off the site.
// Only same-origin paths are honoured. An absolute URL here would make
// /login?next=https://evil.example an open redirect that bounces a
// freshly-signed-in user off the site.
//
// The default lands on the catalogue rather than the hero: someone who just
// signed in came to shop, not to read the pitch again.
function safeNext() {
  const raw = new URLSearchParams(window.location.search).get('next');
  if (!raw) return '/#catalogue';
  if (!raw.startsWith('/') || raw.startsWith('//')) return '/#catalogue';
  return raw;
}

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  errorBox.hidden = true;
  submit.disabled = true;
  submit.textContent = 'Signing in…';

  try {
    const user = await apiFetch('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        phone: document.getElementById('phone').value.trim(),
        password: document.getElementById('password').value,
      }),
    });
    toast(`Welcome back, ${user.name.split(' ')[0]}`);
    const next = safeNext();
    // Staff land on the dashboard; shoppers land on the products.
    window.location.href =
      user.role === 'admin' && next === '/#catalogue' ? '/admin' : next;
  } catch (err) {
    errorBox.textContent = err.message;
    errorBox.hidden = false;
    submit.disabled = false;
    submit.textContent = 'Sign in';
    document.getElementById('password').value = '';
  }
});

mountFooter();
