import { apiFetch, toast } from './cart.js';
import { renderThemeToggle } from './theme.js';
import { mountFooter } from './footer.js';

const form = document.getElementById('form');
const errorBox = document.getElementById('error');
const submit = document.getElementById('submit');

// Same-origin only; a new account lands on the catalogue, not the hero.
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
  submit.textContent = 'Creating account…';

  try {
    const user = await apiFetch('/api/auth/signup', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name: document.getElementById('name').value.trim(),
        phone: document.getElementById('phone').value.trim(),
        email: document.getElementById('email').value.trim(),
        password: document.getElementById('password').value,
      }),
    });
    toast(`Welcome, ${user.name.split(' ')[0]}`);
    window.location.href = safeNext();
  } catch (err) {
    errorBox.textContent = err.message;
    errorBox.hidden = false;
    submit.disabled = false;
    submit.textContent = 'Create account';
  }
});

mountFooter();
renderThemeToggle(document.getElementById('theme-toggle'));
