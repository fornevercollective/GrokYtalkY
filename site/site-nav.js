/**
 * Consistent site header nav for all GrokYtalkY pages.
 * Usage:
 *   <header class="nav" id="site-nav" data-active="burst" data-site-nav>
 *     <!-- optional static fallback: brand + same LINKS below -->
 *   </header>
 *   <script src="site-nav.js"></script>
 *
 * Always rebuilds the header so every page gets the same menu
 * (Home · Burst · Live News · GrokGlyph · Chat · DOJO · Mid-lane · Docs · Install · GitHub).
 *
 * data-active: home|burst|livenews|grokglyph|sphere|phone|chat|dojo|mid-lane|docs|install
 */
(function () {
  'use strict';

  var LINKS = [
    { id: 'home', href: './', label: 'Home' },
    { id: 'burst', href: 'burst.html', label: 'Burst' },
    { id: 'livenews', href: 'livenews.html', label: 'Live News' },
    { id: 'grokglyph', href: 'grokglyph.html', label: 'GrokGlyph' },
    { id: 'sphere', href: 'sphere.html', label: 'Sphere' },
    { id: 'phone', href: 'phone.html', label: 'Phone' },
    { id: 'chat', href: 'chat.html', label: 'Chat' },
    { id: 'dojo', href: 'dojo.html', label: 'DOJO' },
    { id: 'mid-lane', href: 'mid-lane.html', label: 'Mid-lane' },
    { id: 'docs', href: 'docs.html', label: 'Docs' },
    { id: 'install', href: './#install', label: 'Install' },
    {
      id: 'github',
      href: 'https://github.com/fornevercollective/GrokYtalkY',
      label: 'GitHub',
      external: true,
    },
  ];

  function detectActive() {
    var path = (location.pathname || '').split('/').pop() || '';
    path = path.toLowerCase();
    if (!path || path === 'index.html') {
      if ((location.hash || '').toLowerCase() === '#install') return 'install';
      return 'home';
    }
    if (path.indexOf('burst') === 0) return 'burst';
    if (path.indexOf('livenews') === 0 || path.indexOf('live-news') === 0) return 'livenews';
    if (path.indexOf('grokglyph') === 0) return 'grokglyph';
    if (path.indexOf('chat') === 0) return 'chat';
    if (path.indexOf('dojo') === 0) return 'dojo';
    if (path.indexOf('mid-lane') === 0 || path.indexOf('midlane') === 0) return 'mid-lane';
    if (path.indexOf('docs') === 0) return 'docs';
    if (path.indexOf('phone') === 0) return 'phone';
    if (path.indexOf('sphere') === 0) return 'sphere';
    if (path.indexOf('hdri-view') === 0 || path.indexOf('hdri') === 0) return 'sphere';
    if (path.indexOf('glyph-cast') === 0) return 'grokglyph';
    return '';
  }

  function mount(el) {
    if (!el) return;
    var active = (el.getAttribute('data-active') || detectActive() || '').toLowerCase();
    var brandName = el.getAttribute('data-brand') || 'GrokYtalkY';

    var brand = document.createElement('a');
    brand.className = 'brand';
    brand.href = './';
    brand.innerHTML =
      '<span class="brand-mark">◈</span><span class="brand-name"></span>';
    brand.querySelector('.brand-name').textContent = brandName;

    var nav = document.createElement('nav');
    nav.setAttribute('aria-label', 'Site');
    LINKS.forEach(function (link) {
      var a = document.createElement('a');
      a.href = link.href;
      a.textContent = link.label;
      if (link.external) {
        a.rel = 'noopener';
        a.target = '_blank';
      }
      if (link.id === active) {
        a.className = 'nav-active';
        a.setAttribute('aria-current', 'page');
      }
      nav.appendChild(a);
    });

    el.classList.add('nav');
    el.innerHTML = '';
    el.appendChild(brand);
    el.appendChild(nav);
  }

  function run() {
    var nodes = document.querySelectorAll('#site-nav, header.nav[data-site-nav], [data-site-nav]');
    if (nodes.length) {
      nodes.forEach(mount);
      return;
    }
    // legacy: first header.nav without children prepared
    var legacy = document.querySelector('header.nav');
    if (legacy && !legacy.querySelector('nav')) {
      mount(legacy);
    }
  }

  // export for optional manual use
  window.GY_SITE_NAV = { mount: mount, links: LINKS, detectActive: detectActive };

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', run);
  } else {
    run();
  }
})();
