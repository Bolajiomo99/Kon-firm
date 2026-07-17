// Light / dark / system theme.
//
// The CSS drives everything off `prefers-color-scheme`, so an explicit choice
// is expressed by stamping data-theme on <html> and letting a matching rule
// win. That keeps one source of truth for the palette: no second set of
// colours for the toggle to get out of step with.

const KEY = 'konfirm.theme';
export const THEMES = ['system', 'light', 'dark'];

export function currentTheme() {
  try {
    const t = localStorage.getItem(KEY);
    return THEMES.includes(t) ? t : 'system';
  } catch {
    return 'system';
  }
}

export function applyTheme(theme) {
  const root = document.documentElement;
  if (theme === 'system') {
    root.removeAttribute('data-theme');
  } else {
    root.setAttribute('data-theme', theme);
  }
  try {
    localStorage.setItem(KEY, theme);
  } catch {
    // Private browsing refuses writes; the theme still applies for this page.
  }
}

/** Applies the saved theme. Call before first paint to avoid a flash. */
export function initTheme() {
  applyTheme(currentTheme());
}

const ICONS = { system: '🖥️', light: '☀️', dark: '🌙' };
const LABELS = { system: 'System', light: 'Light', dark: 'Dark' };

/** Renders a three-way theme control. */
export function renderThemeToggle(container) {
  if (!container) return;
  container.innerHTML = '';
  container.className = 'theme-toggle';
  container.setAttribute('role', 'group');
  container.setAttribute('aria-label', 'Colour theme');

  for (const t of THEMES) {
    const b = document.createElement('button');
    b.type = 'button';
    b.dataset.theme = t;
    b.title = `${LABELS[t]} theme`;
    b.setAttribute('aria-label', `${LABELS[t]} theme`);
    b.setAttribute('aria-pressed', String(currentTheme() === t));
    b.textContent = ICONS[t];
    b.addEventListener('click', () => {
      applyTheme(t);
      [...container.children].forEach((c) =>
        c.setAttribute('aria-pressed', String(c.dataset.theme === t))
      );
    });
    container.append(b);
  }
}

initTheme();
