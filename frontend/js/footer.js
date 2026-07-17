// The site footer, in one place.
//
// Every page used to carry its own copy, so they drifted: the orders page had
// two lines where the storefront had four columns. Building it from one module
// means a page cannot have a different footer by accident.

const YEAR = 2026;

export function renderFooter(container) {
  if (!container) return;
  container.className = 'site-footer';
  container.innerHTML = '';

  const wrap = document.createElement('div');
  wrap.className = 'wrap';

  const grid = document.createElement('div');
  grid.className = 'footer-grid';

  // Brand column
  const brandCol = document.createElement('div');
  const brand = document.createElement('a');
  brand.className = 'brand';
  brand.href = '/';
  brand.style.marginBottom = '14px';
  const logo = document.createElement('img');
  logo.className = 'logo-img';
  logo.src = '/img/logo-512.png';
  logo.alt = '';
  logo.style.height = '34px';
  const bt = document.createElement('span');
  bt.className = 'brand-text';
  bt.style.fontSize = '1.05rem';
  bt.textContent = 'Kon-firm';
  brand.append(logo, bt);
  const blurb = document.createElement('p');
  blurb.className = 'footer-blurb';
  blurb.textContent =
    'Fashion, gadgets and everyday essentials — sold online and across the counter, ' +
    'from one inventory. Every naira confirmed before dispatch.';
  brandCol.append(brand, blurb);

  const cols = [
    ['Shop', [['All products', '/#catalogue'], ['My orders', '/orders'], ['Create account', '/signup'], ['Sign in', '/login']]],
    ['Help', [['Delivery — Lagos in 48h', '/#catalogue'], ['Returns & refunds', '/#catalogue'], ['Track an order', '/orders']]],
    ['Built with', [['Monnify API', 'https://developers.monnify.com/'], ['Source code', 'https://github.com/Bolajiomo99/Kon-firm']]],
  ];

  grid.append(brandCol);
  for (const [title, links] of cols) {
    const col = document.createElement('div');
    const h = document.createElement('h4');
    h.textContent = title;
    const ul = document.createElement('ul');
    for (const [label, href] of links) {
      const li = document.createElement('li');
      const a = document.createElement('a');
      a.href = href;
      a.textContent = label;
      li.append(a);
      ul.append(li);
    }
    col.append(h, ul);
    grid.append(col);
  }

  const bottom = document.createElement('div');
  bottom.className = 'footer-bottom';
  const left = document.createElement('span');
  left.textContent = `© ${YEAR} Kon-firm · Lagos, Nigeria`;
  const mid = document.createElement('span');
  mid.style.fontSize = '0.76rem';
  mid.textContent = 'Prices include VAT at 7.5%';
  const right = document.createElement('span');
  right.className = 'pay-mark';
  right.textContent = '🔒 Payments secured by Monnify';
  bottom.append(left, mid, right);

  wrap.append(grid, bottom);
  container.append(wrap);
}

/** Replaces whatever footer a page has with the shared one. */
export function mountFooter() {
  let f = document.querySelector('footer.site-footer') || document.querySelector('footer');
  if (!f) {
    f = document.createElement('footer');
    document.body.append(f);
  }
  renderFooter(f);
}
