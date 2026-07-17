// Shared session helpers and the account menu.
import { apiFetch } from './cart.js';

/** Returns the signed-in user, or null. Never throws: a failed session lookup
 *  must degrade to "signed out", not break the page. */
export async function currentUser() {
  try {
    const res = await apiFetch('/api/auth/me');
    return res.authenticated ? res.user : null;
  } catch {
    return null;
  }
}

/** Initials for the avatar. "Zainab Wahab" -> "ZW", "Ada" -> "AD". */
export function initials(name) {
  const parts = String(name || '').trim().split(/\s+/).filter(Boolean);
  if (!parts.length) return '?';
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

/**
 * Renders the account area: a Sign in link when signed out, an avatar with a
 * menu when signed in.
 *
 * This is the only place the account UI is built. Pages used to hardcode nav
 * links as well, which is how the header ended up showing "Admin" and "POS"
 * twice.
 */
export function renderNav(user, container) {
  if (!container) return;
  container.innerHTML = '';

  if (!user) {
    const login = document.createElement('a');
    login.href = '/login';
    login.textContent = 'Sign in';
    login.className = 'btn btn-sm btn-secondary';
    container.append(login);
    return;
  }

  const wrap = document.createElement('div');
  wrap.className = 'account';

  const btn = document.createElement('button');
  btn.className = 'account-btn';
  btn.type = 'button';
  btn.setAttribute('aria-haspopup', 'menu');
  btn.setAttribute('aria-expanded', 'false');
  btn.setAttribute('aria-label', `Account menu for ${user.name}`);

  const av = document.createElement('span');
  av.className = 'avatar';
  av.setAttribute('aria-hidden', 'true');
  av.textContent = initials(user.name);

  const nm = document.createElement('span');
  nm.className = 'account-name';
  nm.textContent = user.name.split(' ')[0];

  btn.append(av, nm);

  const menu = document.createElement('div');
  menu.className = 'menu';
  menu.setAttribute('role', 'menu');
  menu.hidden = true;

  const head = document.createElement('div');
  head.className = 'menu-head';
  const hName = document.createElement('strong');
  hName.textContent = user.name;
  const hPhone = document.createElement('span');
  hPhone.textContent = user.phonePretty || user.phone;
  head.append(hName, hPhone);
  menu.append(head);

  const links = user.role === 'admin'
    ? [['Dashboard', '/admin'], ['Point of sale', '/pos'], ['Store', '/#catalogue']]
    : [['My orders', '/orders']];

  for (const [label, href] of links) {
    const a = document.createElement('a');
    a.href = href;
    a.textContent = label;
    a.setAttribute('role', 'menuitem');
    menu.append(a);
  }

  const out = document.createElement('button');
  out.type = 'button';
  out.className = 'danger';
  out.textContent = 'Sign out';
  out.setAttribute('role', 'menuitem');
  out.addEventListener('click', signOut);
  menu.append(out);

  const close = () => {
    menu.hidden = true;
    btn.setAttribute('aria-expanded', 'false');
  };

  btn.addEventListener('click', (e) => {
    e.stopPropagation();
    menu.hidden = !menu.hidden;
    btn.setAttribute('aria-expanded', String(!menu.hidden));
  });

  // Clicking away or pressing Escape closes it — a menu dismissible only by
  // clicking the same button again feels broken.
  document.addEventListener('click', close);
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') close();
  });
  menu.addEventListener('click', (e) => e.stopPropagation());

  wrap.append(btn, menu);
  container.append(wrap);
}

export async function signOut() {
  try {
    await apiFetch('/api/auth/logout', { method: 'POST' });
  } catch {
    // Send them home regardless: a stuck "Sign out" is worse than an
    // optimistic redirect.
  }
  window.location.href = '/';
}

/** Sends the user to /login, remembering where they were headed. */
export function requireLogin(next) {
  const target = next || window.location.pathname;
  window.location.href = '/login?next=' + encodeURIComponent(target);
}
