// Sample layout before deferred enhancement arrives, then check public and admin pages.
(() => {
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const frames = [];
  const manageStyles = [];
  let initialFrames = 0;
  let done = false;
  window.addEventListener('error', event => report(event.message));
  function sample() {
    if (document.querySelector('#article-content')) {
      if (!document.querySelector('#site-menu').hasAttribute('data-ready')) initialFrames++;
      const selectors = ['.profile', '#main-content', '#article-content', '.account-nav', innerWidth >= 1680 ? '#sidebar-archive' : '#post-archive'];
      if (document.querySelector('[data-media-upload]')) selectors.push('[data-media-upload]', '.media-filters');
      frames.push(selectors.map(selector => {
        const rect = document.querySelector(selector).getBoundingClientRect();
        return [rect.x, rect.y, rect.width, rect.height];
      }));
      const manage = document.querySelector('.account-menu summary');
      if (manage && innerWidth < 768) {
        const style = getComputedStyle(manage);
        manageStyles.push([style.color, style.backgroundColor, getComputedStyle(manage, '::after').transform].join('|'));
      }
    }
    if (!done) requestAnimationFrame(sample);
  }
  requestAnimationFrame(sample);
  window.addEventListener('load', () => setTimeout(() => {
    done = true;
    try {
      if (frames.length < 3 || initialFrames === 0) throw new Error('Not enough samples before navigation enhancement.');
      const first = frames[0];
      for (const frame of frames) {
        frame.forEach((rect, index) => rect.forEach((value, axis) => {
          if (Math.abs(value - first[index][axis]) > 1) throw new Error(`Startup shifted element ${index}, dimension ${axis}: ${first[index][axis]} → ${value}`);
        }));
      }
      if (manageStyles.some(style => style !== manageStyles[0])) throw new Error('Manage changed colors or arrow direction during startup.');
      if (performance.getEntriesByType('resource').some(entry => new URL(entry.name).pathname === '/posts/recent')) throw new Error('Startup fetched Recent Posts separately.');
      if (!sessionStorage.getItem('refresh-admin')) {
        sessionStorage.setItem('refresh-admin', 'true');
        document.querySelector('#email').value = 'trentnix@gmail.com';
        document.querySelector('#password').value = 'Sample-Password-123!';
        document.querySelector('#login-form').requestSubmit();
      } else {
        if (!document.querySelector('.account-menu')) throw new Error('Admin refresh was not tested.');
        const stage = sessionStorage.getItem('refresh-admin');
        if (stage === 'true') {
          sessionStorage.setItem('refresh-admin', 'reloaded');
          location.reload();
        } else if (stage === 'reloaded') {
          sessionStorage.setItem('refresh-admin', 'media');
          location.assign('/media');
        } else if (stage === 'media') {
          sessionStorage.setItem('refresh-admin', 'media-reloaded');
          location.reload();
        } else {
          const upload = document.querySelector('[data-media-upload]');
          if (!upload || upload.querySelector('details').open || upload.getBoundingClientRect().height < 44) throw new Error('Media upload must start as a visible, collapsed disclosure.');
          document.documentElement.classList.remove('js');
          if (upload.getClientRects().length) throw new Error('The JavaScript upload form must stay hidden without enhancement.');
          document.documentElement.classList.add('js');
          report('PASS');
        }
      }
    } catch (error) {
      report(error.message);
    }
  }, 150), {once: true});
})();
