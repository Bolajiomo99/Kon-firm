import { apiFetch, toast } from './cart.js';

const form = document.getElementById('form');
const errorBox = document.getElementById('error');
const submit = document.getElementById('submit');

// Only same-origin paths are honoured as a redirect target. Accepting an
// absolute URL here would make this an open redirect: a link to
// /login?next=https://evil.example would bounce a freshly-signed-in user
// straight off the site.
function safeNext() {
  const raw = new URLSearchParams(window.location.search).get('next');
  if (!raw) return '/';
  if (!raw.startsWith('/') || raw.startsWith('//')) return '/';
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
    window.location.href = user.role === 'admin' && next === '/' ? '/admin' : next;
  } catch (err) {
    errorBox.textContent = err.message;
    errorBox.hidden = false;
    submit.disabled = false;
    submit.textContent = 'Sign in';
    document.getElementById('password').value = '';
  }
});
