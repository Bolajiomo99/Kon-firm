// Shared session helpers.
import { apiFetch } from './cart.js';

/** Returns the signed-in user, or null. Never throws: a failed session
 *  lookup must degrade to "signed out", not break the page. */
export async function currentUser() {
  try {
    const res = await apiFetch('/api/auth/me');
    return res.authenticated ? res.user : null;
  } catch {
    return null;
  }
}

/** Renders the account area of the nav to match the session. */
export function renderNav(user, container) {
  if (!container) return;
  container.innerHTML = '';

  if (!user) {
    const login = document.createElement('a');
    login.href = '/login';
    login.textContent = 'Sign in';
    container.append(login);
    return;
  }

  if (user.role === 'admin') {
    const admin = document.createElement('a');
    admin.href = '/admin';
    admin.textContent = 'Admin';
    container.append(admin);

    const pos = document.createElement('a');
    pos.href = '/pos';
    pos.textContent = 'POS';
    container.append(pos);
  } else {
    const orders = document.createElement('a');
    orders.href = '/orders';
    orders.textContent = 'My orders';
    container.append(orders);
  }

  const out = document.createElement('button');
  out.className = 'btn btn-sm btn-secondary';
  out.type = 'button';
  out.textContent = 'Sign out';
  out.addEventListener('click', signOut);
  container.append(out);
}

export async function signOut() {
  try {
    await apiFetch('/api/auth/logout', { method: 'POST' });
  } catch {
    // Even if the call fails, send them home: the cookie is cleared
    // server-side on any successful logout, and a stuck "Sign out" button
    // is worse than an optimistic redirect.
  }
  window.location.href = '/';
}

/** Sends the user to /login, remembering where they were headed. */
export function requireLogin(next) {
  const target = next || window.location.pathname;
  window.location.href = '/login?next=' + encodeURIComponent(target);
}
