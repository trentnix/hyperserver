// Track document listeners independently of editor DOM nodes. Detached editors
// must not remain reachable through EasyMDE's keyboard handler.
const editorKeyListeners = new Set();
const originalAddListener = document.addEventListener.bind(document);
const originalRemoveListener = document.removeEventListener.bind(document);
document.addEventListener = function(type, callback, options) {
  if (type === 'keydown') editorKeyListeners.add(callback);
  return originalAddListener(type, callback, options);
};
document.removeEventListener = function(type, callback, options) {
  if (type === 'keydown') editorKeyListeners.delete(callback);
  return originalRemoveListener(type, callback, options);
};

(async () => {
  // HTMX binds its initial handlers on DOMContentLoaded, after deferred scripts.
  await new Promise(resolve => document.addEventListener('DOMContentLoaded', () => setTimeout(resolve, 0), {once: true}));
  const report = message => fetch('/test/result', {method: 'POST', body: message});
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const wait = async predicate => {
    const end = Date.now() + 12000;
    while (Date.now() < end) {
      if (predicate()) return;
      await new Promise(resolve => setTimeout(resolve, 25));
    }
    throw new Error('Timed out at ' + location.pathname + ': ' + predicate.toString());
  };
  const savePublication = async (edit, published, nextStage) => {
    edit.click();
    await wait(() => document.querySelector('dialog[open] #post-form .CodeMirror'));
    await wait(() => !document.querySelector('#dialog-host').classList.contains('htmx-settling'));
    const form = document.querySelector('#post-form');
    assert(form.querySelector('.publish-option').textContent.trim() === 'Publish', 'Editor must label publication consistently.');
    form.elements.published.checked = published;
    form.elements.published.dispatchEvent(new Event('change', {bubbles: true}));
    sessionStorage.setItem('blog-test-stage', nextStage);
    form.requestSubmit();
  };
  window.alert = message => report('Unexpected editor alert: ' + message);
  window.addEventListener('error', event => report('Browser error: ' + event.message));
  try {
    await document.fonts.ready;
    for (const style of ['400', 'italic 600']) {
      const faces = await document.fonts.load(style + ' 16px "Source Sans 3"');
      assert(faces.length > 0 && faces.every(face => face.status === 'loaded'), 'Source Sans 3 must load: ' + style);
    }
    const requestedWidth = new URLSearchParams(location.search).get('test-width');
    if (requestedWidth) assert(innerWidth === Number(requestedWidth), 'Wrong test viewport: ' + innerWidth + ' instead of ' + requestedWidth);
    const stage = sessionStorage.getItem('blog-test-stage');
    if (!stage) {
      // Firefox can reuse fonts across documents without recording another request.
      const fontRequests = performance.getEntriesByType('resource').filter(entry => new URL(entry.name).pathname.endsWith('.woff2'));
      assert(fontRequests.some(entry => new URL(entry.name).pathname.endsWith('/source-sans-3.woff2')), 'Upright font must load locally.');
      assert(fontRequests.some(entry => new URL(entry.name).pathname.endsWith('/source-sans-3-italic.woff2')), 'Italic font must load locally.');
      assert(!performance.getEntriesByType('resource').some(entry => new URL(entry.name).origin !== location.origin), 'Blog assets must not require external requests.');
      await wait(() => document.querySelector('.archive-nav .post-archive'));
      const archiveTitle = document.querySelector('.archive-heading h2').getBoundingClientRect();
      const feedIcon = document.querySelector('.archive-heading img').getBoundingClientRect();
      assert(Math.abs((archiveTitle.top + archiveTitle.bottom) / 2 - (feedIcon.top + feedIcon.bottom) / 2) < 1, 'RSS icon must be vertically centered with Recent Posts.');
      const profile = document.querySelector('.profile');
      const portrait = profile.querySelector('img').getBoundingClientRect();
      const bio = document.querySelector('.profile-details').getBoundingClientRect();
      const article = document.querySelector('article').getBoundingClientRect();
      const archive = document.querySelector('.archive-nav').getBoundingClientRect();
      const archiveStyle = getComputedStyle(document.querySelector('.archive-nav'));
      const archivePadding = innerWidth < 768 || innerWidth >= 1680 ? '20px' : '40px';
      assert(archiveStyle.paddingLeft === archivePadding && archiveStyle.paddingRight === archivePadding, 'Recent Posts has incorrect horizontal padding.');
      if (innerWidth >= 1680) assert(archiveStyle.paddingTop === '50px', 'Sidebar Recent Posts needs 50px of separation from site navigation.');
      const sidebar = document.querySelector('.site-sidebar');
      const footer = document.querySelector('.site-footer');
      assert(getComputedStyle(document.querySelector('.article-column')).paddingBottom === (innerWidth < 1680 ? '80px' : '0px'), 'Extra spacing after the article must apply only below the ultrawide breakpoint.');
      const links = document.querySelector('.site-links');
      const header = document.querySelector('.site-header');
      const main = document.querySelector('#main-content');
      const mainStyle = getComputedStyle(main);
      const pageTop = document.querySelector('.page-top');
      const backgroundStyle = getComputedStyle(innerWidth >= 1680 ? document.body : pageTop);
      assert(backgroundStyle.backgroundImage.includes('/assets/img/site-bg.jpg'), 'Page must retain its background image at every width.');
      assert(backgroundStyle.backgroundAttachment === 'fixed' && backgroundStyle.backgroundPositionY === '100%', 'Background must stay pinned to the bottom of the viewport.');
      assert(mainStyle.backgroundImage === 'none', 'Content must not duplicate the viewport background.');
      if (innerWidth < 1680) {
        assert(getComputedStyle(document.body).backgroundImage === 'none', 'The illustration must not extend behind the footer.');
        assert(!pageTop.contains(footer), 'Footer must sit outside the illustration container.');
      }
      const menuToggle = document.querySelector('.menu-toggle');
      const profileLinks = document.querySelector('.profile-links');
      assert([...profileLinks.querySelectorAll('a')].map(link => link.textContent).join(' ') === 'About Me Projects' && !profileLinks.querySelector('img'), 'Profile navigation must use stored page titles as text links.');
      assert(header.contains(links), 'Site links must belong to the header.');
      assert(getComputedStyle(footer).backgroundColor === 'rgba(0, 0, 0, 0)', 'Footer must have no background color.');
      if (innerWidth < 768) {
        assert(profileLinks.getClientRects().length === 0, 'Mobile must hide duplicate profile navigation.');
        assert(!menuToggle.hidden && links.hidden, 'Mobile menu must start collapsed.');
        assert(menuToggle.getAttribute('aria-expanded') === 'false', 'Collapsed menu must report its state.');
        assert(document.querySelector('.header-menu').getBoundingClientRect().top === 0, 'Mobile navigation bar must sit at the top of the page.');
        assert(menuToggle.getBoundingClientRect().bottom <= profile.getBoundingClientRect().top, 'Mobile menu must precede the profile.');
        menuToggle.click();
        assert(!links.hidden && menuToggle.getAttribute('aria-expanded') === 'true', 'Menu button must reveal links.');
        links.querySelector('a').focus();
        links.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
        assert(links.hidden && document.activeElement === menuToggle, 'Escape must close the menu and return focus.');
        menuToggle.click();
      } else if (innerWidth < 1680) {
        assert(getComputedStyle(profileLinks).marginTop === '16px', 'Flat navigation must retain its spacing below the bio.');
        assert(document.querySelector('.header-menu').getClientRects().length === 0, 'Desktop must hide the separate menu.');
        assert(profileLinks.getClientRects().length > 0 && profileLinks.getBoundingClientRect().top >= profile.querySelector('p').getBoundingClientRect().bottom, 'Desktop links must appear below the bio.');
      } else {
        assert(profileLinks.getClientRects().length === 0 && menuToggle.hidden, 'Ultrawide must hide compact links and the hamburger.');
        assert(!links.hidden && links.getBoundingClientRect().top >= profile.getBoundingClientRect().bottom, 'Ultrawide navigation must sit below the bio.');
      }
      const siteLinks = links.querySelectorAll('a');
      if (innerWidth < 768) {
        assert(siteLinks[1].getBoundingClientRect().top >= siteLinks[0].getBoundingClientRect().bottom, 'Mobile links must form a vertical list.');
        const nav = document.querySelector('.header-menu');
        assert(getComputedStyle(nav).backgroundColor !== getComputedStyle(sidebar).backgroundColor, 'Mobile navigation needs a distinct background.');
        assert(parseFloat(getComputedStyle(menuToggle).borderTopWidth) > 0 && menuToggle.getBoundingClientRect().height >= 44, 'Mobile menu needs a bordered, touch-sized control.');
        assert(siteLinks[0].getBoundingClientRect().height >= 44, 'Mobile menu links need touch-sized targets.');
      }
      if (innerWidth < 768) menuToggle.click();
      assert(document.querySelectorAll('#post-archive').length === 1, 'Recent Posts must have one persistent container.');
      const pageWidth = Math.max(320, document.documentElement.clientWidth);
      assert(portrait.right <= bio.left, 'Profile photo must sit beside the bio.');
      assert(Math.abs(portrait.top - bio.top) <= 5, 'Profile photo and bio are not top-aligned.');
      assert(getComputedStyle(document.body).fontSize === '16px', 'Base typography changed.');
      const articleStyle = getComputedStyle(document.querySelector('article'));
      const contentPadding = innerWidth < 768 ? '20px' : '60px';
      assert(articleStyle.paddingLeft === contentPadding && articleStyle.paddingRight === contentPadding, 'Article has incorrect horizontal padding.');
      const textSize = innerWidth >= 1680 ? 19.2 : 17.6;
      const lineHeight = innerWidth >= 1680 ? 28.8 : 24.64;
      // Allow subpixel rounding across browsers without hiding a breakpoint change.
      const typographyTolerance = .1;
      assert(Math.abs(parseFloat(articleStyle.fontSize) - textSize) <= typographyTolerance, `Article font size at ${innerWidth}px: got ${articleStyle.fontSize}, expected ${textSize}px ± ${typographyTolerance}px.`);
      assert(Math.abs(parseFloat(articleStyle.lineHeight) - lineHeight) <= typographyTolerance, `Article line height at ${innerWidth}px: got ${articleStyle.lineHeight}, expected ${lineHeight}px ± ${typographyTolerance}px.`);
      assert(document.documentElement.scrollWidth <= pageWidth, 'Page exceeds its minimum width or viewport.');
      assert(Math.abs(article.width - Math.min(pageWidth, 960)) < 1, 'Article does not respect its maximum width.');
      assert(Math.abs((article.left + article.right) / 2 - pageWidth / 2) < 1, 'Article is not centered on the screen.');
      assert(getComputedStyle(document.querySelector('article')).textAlign === 'left', 'Article text must be left-aligned.');
      if (innerWidth < 768) {
        assert(portrait.width === 80, 'Mobile portrait must retain its size.');
      } else {
        assert(portrait.width === (innerWidth >= 1680 ? 120 : 100), 'Portrait must be 120px in the sidebar and 100px in the desktop header.');
        assert(Math.abs(portrait.width - portrait.height) < 1, 'Desktop portrait must remain circular: ' + portrait.width + ' x ' + portrait.height);
      }
      assert(footer.getBoundingClientRect().top >= document.querySelector('article').getBoundingClientRect().bottom - 1, 'Footer must follow the article: ' + footer.getBoundingClientRect().top + ' < ' + document.querySelector('article').getBoundingClientRect().bottom);
      if (innerWidth < 1680) {
        assert(sidebar.getBoundingClientRect().bottom <= article.top, 'Bio must precede the article when stacked.');
        assert(archive.top >= document.querySelector('#main-content').getBoundingClientRect().bottom - 1, 'Recent Posts must follow the content.');
        assert(footer.contains(document.querySelector('#post-archive')), 'Recent Posts must be inside the footer.');
        const footerBounds = footer.getBoundingClientRect();
        assert(Math.abs(footerBounds.left) < 1 && Math.abs(footerBounds.width - pageWidth) < 1, 'Footer background must span the viewport.');
        assert(footerBounds.bottom >= innerHeight, 'Footer background must reach the bottom of the viewport.');
        assert(getComputedStyle(sidebar).position === 'static', 'Stacked navigation must scroll with the page.');
      } else {
        assert(getComputedStyle(sidebar).position === 'sticky', 'Desktop sidebar must stay visible while reading.');
        const sidebarBounds = sidebar.getBoundingClientRect();
        assert(sidebar.contains(document.querySelector('#post-archive')), 'Desktop archive must remain inside the sidebar.');
        assert(Math.abs(sidebarBounds.left) < 1 && Math.abs(sidebarBounds.right - article.left) < 1, 'Sidebar must fill the entire left section.');
        assert(Math.abs(header.getBoundingClientRect().width - sidebar.clientWidth) < 1, 'Header must use the available sidebar width.');
        assert(Math.abs(archive.width - sidebar.clientWidth) < 1, 'Navigation must use the available sidebar width.');
      }
      if (new URLSearchParams(location.search).has('test-layout')) {
        // Exercise natural sizes, linked images, and text sharing an image paragraph.
        const imagePost = document.createElement('div');
        imagePost.className = 'post-body';
        document.querySelector('article').append(imagePost);
        for (const size of ['large', 'small', 'tall', 'wide', 'square', 'tiny']) {
          const paragraph = document.createElement('p');
          const before = document.createTextNode('Text before the image.');
          const after = document.createTextNode('Text after the image.');
          const image = document.createElement('img');
          image.src = '/test/image.svg?size=' + size;
          const link = document.createElement('a');
          link.href = image.src;
          link.append(image);
          paragraph.append(before, size === 'wide' ? link : image, after);
          imagePost.append(paragraph);
          await wait(() => image.complete && image.naturalWidth > 0);
          const bounds = image.getBoundingClientRect();
          // Large fixtures can introduce a scrollbar and move the centered column.
          const articleBounds = document.querySelector('article').getBoundingClientRect();
          const textWidth = articleBounds.width - 2 * parseFloat(contentPadding);
          const limit = innerWidth < 1680 ? articleBounds.width : textWidth;
          const expectedWidth = Math.min(image.naturalWidth, limit);
          assert(Math.abs(bounds.width - expectedWidth) < 1, `${size} image width at ${innerWidth}px: ${bounds.width}, expected ${expectedWidth}.`);
          assert(Math.abs((bounds.left + bounds.right) / 2 - (articleBounds.left + articleBounds.right) / 2) < 1, `${size} image is not centered.`);
          assert(Math.abs(bounds.height - 20 - expectedWidth * image.naturalHeight / image.naturalWidth) < 1, `${size} image aspect ratio changed.`);
          for (const text of [before, after]) {
            const range = document.createRange();
            range.selectNodeContents(text);
            const textBounds = range.getBoundingClientRect();
            assert(textBounds.left >= articleBounds.left + parseFloat(contentPadding) - 1 && textBounds.right <= articleBounds.right - parseFloat(contentPadding) + 1, 'Image styling moved text into the gutters.');
          }
          assert(document.documentElement.scrollWidth <= Math.max(320, document.documentElement.clientWidth), `${size} image caused horizontal overflow at ${innerWidth}px.`);
        }
        imagePost.remove();
        if (innerWidth === 768) {
          frameElement.style.width = '767px';
          await wait(() => !menuToggle.hidden && links.hidden);
          assert(profileLinks.getClientRects().length === 0, 'Resizing to mobile must hide profile links.');
          menuToggle.click();
          assert(!links.hidden, 'Resized mobile menu must open.');
          frameElement.style.width = '768px';
          await wait(() => menuToggle.hidden && !links.hidden);
          assert(profileLinks.getClientRects().length > 0 && document.querySelector('.header-menu').getClientRects().length === 0, 'Resizing to desktop must restore only profile links.');
          frameElement.style.width = '767px';
          await wait(() => !menuToggle.hidden && links.hidden);
          frameElement.style.width = '768px';
          await wait(() => menuToggle.hidden && !links.hidden);
        }
        const content = document.querySelector('article');
        const longText = document.createElement('p');
        longText.textContent = 'LongUnbrokenPostContent'.repeat(100);
        const code = document.createElement('pre');
        code.textContent = longText.textContent;
        content.append(longText, code);
        assert(document.documentElement.scrollWidth <= pageWidth, 'Long post content widens the page.');
        assert(code.scrollWidth > code.clientWidth, 'Wide code must scroll inside its own block.');
        assert(document.body.getBoundingClientRect().width >= 320, 'Page shrank below the mobile minimum.');
        if (innerWidth < 1680) {
          assert(document.querySelector('#post-archive').getBoundingClientRect().top >= content.getBoundingClientRect().bottom - 1, 'Long content overlaps Recent Posts.');
          content.textContent = 'Short page';
          assert(Math.abs(footer.getBoundingClientRect().bottom - innerHeight) < 1, 'Short pages must fill the viewport without a white gap below the footer.');
          assert(Math.abs(footer.getBoundingClientRect().top - (innerHeight - 200)) < 1, 'Short pages must initially show 200px of the footer.');

          // A long archive extends the document, not its own scroll container.
          const entries = document.createElement('ul');
          for (let index = 0; index < 60; index++) {
            const item = document.createElement('li');
            const link = document.createElement('a');
            link.href = '/post/s/example-' + index;
            link.textContent = 'Example post ' + index;
            item.append(link);
            entries.append(item);
          }
          document.querySelector('.post-archive').append(entries);
          assert(Math.abs(footer.getBoundingClientRect().top - (innerHeight - 200)) < 1, 'A long archive must not pull the footer upward.');
          assert(footer.getBoundingClientRect().height > 200, 'Footer clipped the long archive.');
          assert(getComputedStyle(footer).overflowY === 'visible', 'Footer must not have an internal scrollbar.');
          entries.lastElementChild.scrollIntoView({block: 'end'});
          await wait(() => entries.lastElementChild.getBoundingClientRect().bottom <= innerHeight + 1);
          assert(scrollY > 0 && footer.scrollTop === 0, 'Scrolling the archive must scroll the page.');

          content.style.minHeight = innerHeight + 'px';
          scrollTo(0, 0);
          await wait(() => scrollY === 0);
          assert(footer.getBoundingClientRect().top >= innerHeight, 'Long content must push the footer below the viewport.');
          assert(footer.getBoundingClientRect().top >= content.getBoundingClientRect().bottom - 1, 'Footer overlaps long content: ' + footer.getBoundingClientRect().top + ' < ' + content.getBoundingClientRect().bottom);
          entries.lastElementChild.scrollIntoView({block: 'end'});
          await wait(() => entries.lastElementChild.getBoundingClientRect().bottom <= innerHeight + 1);
          assert(scrollY > 0 && footer.scrollTop === 0, 'Long content and archive must share normal page scrolling.');
        }
        await report('PASS');
        return;
      }
      const email = document.querySelector('#email');
      const password = document.querySelector('#password');
      assert(!document.querySelector('a[href="/auth/register/email"]'), 'Registered administrator must not see a registration link.');
      assert(email && password, 'Missing login controls.');
      const formWidth = document.querySelector('#login-form').getBoundingClientRect().width;
      const availableWidth = article.width - parseFloat(articleStyle.paddingLeft) - parseFloat(articleStyle.paddingRight);
      assert(Math.abs(formWidth - Math.min(540, availableWidth)) < 1, 'Login form must remain centered and bounded without sizing its wrapper.');
      assert(Math.abs(email.getBoundingClientRect().width - password.getBoundingClientRect().width) < 1, 'Email and password widths differ.');
      assert(getComputedStyle(email).paddingLeft === '12px', 'Email field has no blog styling.');

      // Use a real fragment response to exercise out-of-band notification delivery.
      const target = document.createElement('div');
      document.body.append(target);
      await htmx.ajax('GET', '/about?test-notifications=1', {target});
      target.remove();
      const success = document.querySelector('.notification--success');
      const failure = document.querySelector('.notification:not(.notification--success)');
      assert(success && failure, 'HTMX notifications did not reach the page.');
      assert(getComputedStyle(success).animationDelay === '5s', 'Success notification has no reading delay.');
      if (innerWidth === 390) {
        assert(matchMedia('(prefers-reduced-motion: reduce)').matches, 'Reduced-motion test preference is not active.');
        assert(parseFloat(getComputedStyle(success).animationDuration) < .01, 'Reduced motion must skip the fade, not the reading delay.');
      }
      const dismiss = success.querySelector('button');
      dismiss.focus();
      assert(getComputedStyle(success).animationPlayState === 'paused', 'Keyboard focus must pause dismissal.');
      await new Promise(resolve => setTimeout(resolve, 5500));
      assert(success.isConnected, 'Focused notification disappeared.');
      email.focus();
      await wait(() => !success.isConnected);
      assert(failure.isConnected, 'Error notification disappeared automatically.');
      failure.querySelector('button').click();
      assert(!failure.isConnected, 'Manual dismissal failed.');
      assert(!document.querySelector('#notifications').textContent.trim(), 'Dismissed notification content remains.');
      email.value = 'draft@example.test';
      password.value = 'unsaved password';
      await htmx.ajax('POST', '/test/plain-error', {target: '#login'});
      const notice = document.querySelector('#login [data-request-error]');
      assert(notice?.textContent.includes('<img'), 'Plain-text error was not displayed.');
      assert(!notice.querySelector('img') && !window.errorInjected, 'Plain text was interpreted as HTML.');
      assert(document.querySelector('#email') === email && email.value === 'draft@example.test' && password.value === 'unsaved password', 'Error discarded login inputs.');
      email.value = 'trentnix@gmail.com';
      password.value = 'Sample-Password-123!';
      sessionStorage.setItem('blog-test-stage', 'editor');
      document.querySelector('#login-form').requestSubmit();
      return;
    }
    if (stage === 'editor') {
      const loginNotice = document.querySelector('.notification--success');
      assert(loginNotice?.textContent.includes('logged in'), 'Login notification is missing.');
      await wait(() => !loginNotice.isConnected);
      const posts = document.querySelector('.account-nav a[href="/post/manage"]');
      assert(posts && !document.querySelector('.account-nav a[href="/post/create"]'), 'Account navigation must link to Posts, not New post.');
      if (innerWidth < 768) document.querySelector('.account-menu summary').click();
      const logoutStyle = getComputedStyle(document.querySelector('.account-nav button'));
      const navStyle = getComputedStyle(posts);
      for (const property of ['color', 'textDecorationLine', 'fontSize', 'lineHeight', 'transition']) {
        assert(logoutStyle[property] === navStyle[property], 'Log out differs from navigation links in ' + property);
      }
      sessionStorage.setItem('blog-test-stage', 'posts');
      posts.click();
      return;
    }
    if (stage === 'posts') {
      assert(document.querySelector('#draft-posts') && document.querySelector('#published-posts'), 'Post management sections are missing.');
      const link = document.querySelector('a[href="/post/create"]');
      assert(link, 'Posts page has no New post link.');
      link.click();
      await wait(() => document.querySelector('dialog[open] .CodeMirror'));
      const cm = document.querySelector('.CodeMirror').CodeMirror;
      const panelStyle = getComputedStyle(document.querySelector('.editor-panel'));
      assert(panelStyle.fontSize === '16px' && panelStyle.lineHeight === '24px', 'Modal editor typography is inconsistent.');
      const saveStyle = getComputedStyle(document.querySelector('#post-form [type=submit]'));
      const cancelStyle = getComputedStyle(document.querySelector('#post-form .button-secondary'));
      assert(saveStyle.backgroundColor !== cancelStyle.backgroundColor && cancelStyle.textDecorationLine === 'none', 'Cancel must be a secondary control.');
      assert(document.querySelector('.CodeMirror').getBoundingClientRect().width > 200, 'Editor initialized while hidden.');
      document.querySelector('#post-title').value = 'Browser post';
      document.querySelector('#post-slug').value = 'browser-post';
      cm.setValue('# Preview heading');
      document.querySelector('.tool-preview').click();
      await wait(() => document.querySelector('.editor-preview h1')?.textContent === 'Preview heading');
      document.querySelector('.tool-preview').click();

      // Empty Markdown must be rejected by the server without replacing the editor.
      cm.setValue('');
      document.querySelector('#post-form').requestSubmit();
      await wait(() => document.querySelector('#editor-errors').textContent.includes('valid Markdown'));
      assert(getComputedStyle(document.querySelector('#editor-errors')).color === 'rgb(161, 38, 38)', 'Editor errors must use the shared error style.');
      assert(document.querySelector('.CodeMirror').CodeMirror === cm, 'Save failure replaced the editor.');
      cm.setValue('# Saved heading\n\n');
      cm.setCursor(cm.lineCount(), 0);

      // Exercise EasyMDE's actual upload control, not a separate fetch shortcut.
      const canvas = document.createElement('canvas');
      canvas.width = canvas.height = 2;
      const blob = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
      document.querySelector('.tool-image').click();
      const input = document.querySelector('.imageInput');
      const transfer = new DataTransfer();
      transfer.items.add(new File([blob], 'sample.png', {type: 'image/png'}));
      input.files = transfer.files;
      input.dispatchEvent(new Event('change'));
      await wait(() => cm.getValue().includes('/images/'));
      assert(!cm.getValue().includes(location.origin + '//'), 'Image URL has a doubled slash.');
      sessionStorage.setItem('blog-test-stage', 'saved');
      document.querySelector('#post-form').requestSubmit();
      return;
    }
    if (stage === 'saved') {
      assert(document.querySelector('.post-heading').textContent.includes('Draft preview'), 'New posts must open a private preview.');
      assert((await fetch('/post/s/browser-post', {credentials: 'omit'})).status === 404, 'Anonymous readers can view a draft.');
      const posts = await fetch('/post/manage');
      assert(posts.ok && (await posts.text()).includes('/post/s/browser-post'), 'Saved draft is missing from the Posts page.');
      await savePublication(document.querySelector('.content-actions .action-edit'), true, 'published');
      return;
    }
    if (stage === 'published') {
      assert(location.pathname === '/post/s/browser-post', 'Save did not navigate to the post.');
      assert(!document.querySelector('.post-heading').textContent.includes('Draft preview'), 'Publishing retained draft status.');
      assert((await fetch('/post/s/browser-post', {credentials: 'omit'})).ok, 'Published post is not public.');
      assert(document.querySelector('.post-body h1')?.textContent === 'Saved heading', 'Published Markdown differs from preview.');
      assert(getComputedStyle(document.querySelector('.post-heading')).textAlign === 'left', 'Post heading must align with its content.');
      const actions = [...document.querySelectorAll('.content-actions a, .content-actions button')];
      assert(actions.map(action => action.textContent).join(',') === 'Edit,Delete', 'Published post actions are incorrect.');
      const actionStyle = getComputedStyle(actions[0]);
      assert(actionStyle.textDecorationLine === 'none', 'Post actions should not look like underlined content links.');
      const menu = document.querySelector('.content-actions .action-menu');
      const more = menu.querySelector('summary');
      assert(!menu.open, 'Secondary actions must start collapsed.');
      assert(Math.abs(more.getBoundingClientRect().top - actions[0].getBoundingClientRect().top) < 1, 'Edit and More must share a row.');
      more.click();
      for (const action of actions) {
        assert(action.getBoundingClientRect().height >= (innerWidth < 768 ? 44 : 36), 'Post action is too small to interact with.');
      }
      assert(actions[1].matches('.action-delete') && getComputedStyle(actions[1]).borderTopStyle === 'solid', 'Delete must be separated at the end of More.');
      assert(actionStyle.backgroundColor !== getComputedStyle(actions[1]).backgroundColor, 'Edit must be emphasized.');
      const image = document.querySelector('.post-body img');
      assert(image, 'Published post has no image: ' + document.querySelector('.post-body').innerHTML);
      await wait(() => image?.complete);
      assert(image.naturalWidth === 2, 'Uploaded image did not load.');
      const baselineListeners = editorKeyListeners.size;
      document.querySelector('a[href^="/post/update/"]').click();
      await wait(() => document.querySelector('dialog[open] .CodeMirror'));
      document.querySelector('[data-close-dialog]').click();
      assert(!document.querySelector('dialog[open]'), 'Cancel did not close the dialog.');
      await htmx.ajax('GET', '/posts/recent', '.archive-nav');
      assert(!document.querySelector('dialog[open]'), 'An unrelated swap reopened the dialog.');
      document.querySelector('a[href^="/post/update/"]').click();
      await wait(() => document.querySelector('dialog[open] .CodeMirror'));
      assert(document.querySelectorAll('.CodeMirror').length === 1, 'Reopening duplicated the editor.');
      assert(editorKeyListeners.size === baselineListeners + 1, 'Replacing an editor leaked its document listener.');

      const cm = document.querySelector('.CodeMirror').CodeMirror;
      const form = document.querySelector('#post-form');
      cm.setValue('An unsaved draft');
      const title = document.querySelector('#post-title');
      title.value = 'An unsaved title';
      const unchanged = () => {
        assert(document.querySelector('.CodeMirror').CodeMirror === cm && cm.getValue() === 'An unsaved draft', 'Error replaced the editor or lost the draft.');
        assert(document.querySelector('#post-title') === title && title.value === 'An unsaved title', 'Error lost the title.');
      };
      for (let attempt = 0; attempt < 20; attempt++) {
        await fetch('/auth/login/email', {method: 'POST', body: new URLSearchParams({email: 'trentnix@gmail.com', password: 'unused'})});
      }
      await htmx.ajax('POST', '/auth/login/email', {target: '#editor-errors', values: {email: 'trentnix@gmail.com', password: 'unused'}});
      assert(form.querySelector('[data-request-error]')?.textContent.includes('Too many requests'), 'Rate limiting produced no feedback.');
      unchanged();

      await htmx.ajax('POST', '/test/json-error', {target: '#editor-errors'});
      assert(form.querySelector('[data-request-error]').textContent.includes('HTTP 503'), 'Non-text failure lacks a fallback message.');
      assert(!form.textContent.includes('private detail'), 'Non-text response exposed private details.');
      unchanged();

      await htmx.ajax('POST', '/test/disconnect', {target: '#editor-errors'}).catch(() => {});
      await wait(() => form.querySelector('[data-request-error]')?.textContent.includes('connection failed'));
      unchanged();

      form.querySelector('[data-request-error]').remove();
      // Use a real XHR timeout, with a short timeout on this test-only button.
      const button = document.createElement('button');
      button.type = 'button';
      button.setAttribute('hx-post', '/test/timeout');
      button.setAttribute('hx-request', '{"timeout":30}');
      button.setAttribute('hx-target', '#editor-errors');
      form.append(button);
      htmx.process(button);
      button.click();
      await wait(() => form.querySelector('[data-request-error]')?.textContent.includes('connection failed'));
      unchanged();
      const originalConfirm = window.confirm;
      let leavePrompt = '';
      window.confirm = message => { leavePrompt = message; return true; };
      document.querySelector('[data-close-dialog]').click();
      window.confirm = originalConfirm;
      assert(leavePrompt.includes('without saving'), 'Closing a changed editor must warn about unsaved edits.');
      document.querySelector('.content-actions .action-menu summary').click();
      document.querySelector('a[href^="/post/delete/"]').click();
      await wait(() => document.querySelector('dialog[open] form[action^="/post/delete/"]'));
      const deletion = document.querySelector('dialog[open] form');
      const confirm = deletion.querySelector('button[type="submit"]');
      const cancel = deletion.querySelector('[data-close-dialog]');
      assert(getComputedStyle(deletion).display === 'flex', 'Delete controls lost their action-row layout.');
      assert(confirm.classList.contains('button-danger') && cancel.classList.contains('button-secondary'), 'Delete confirmation needs destructive and secondary controls.');
      assert(getComputedStyle(confirm).backgroundColor !== getComputedStyle(cancel).backgroundColor, 'Delete and Cancel must not look alike.');
      assert(Math.abs(confirm.getBoundingClientRect().top - cancel.getBoundingClientRect().top) < 1, 'Delete controls are not aligned.');
      cancel.click();
      assert(!document.querySelector('dialog[open]'), 'Cancel did not close the delete dialog.');
      sessionStorage.setItem('blog-test-stage', 'manage-unpublish');
      location.assign('/post/manage');
      return;
    }
    if (stage === 'manage-unpublish') {
      assert(!document.querySelector('#published-posts form'), 'Posts list must not offer publication shortcuts.');
      assert(document.documentElement.scrollWidth <= innerWidth, 'Post management overflows the viewport.');
      await savePublication(document.querySelector('#published-posts a[href^="/post/update/"]'), false, 'unpublished');
      return;
    }
    if (stage === 'unpublished') {
      assert(location.pathname === '/post/s/browser-post', 'Saving a draft must return to its preview.');
      const response = await fetch('/post/manage');
      assert(response.ok, 'Unable to read post management.');
      const list = new DOMParser().parseFromString(await response.text(), 'text/html');
      const draft = list.querySelector('#draft-posts a[href="/post/s/browser-post"]');
      assert(draft && !list.querySelector('#published-posts a[href="/post/s/browser-post"]'), 'Post did not move from Published to Drafts.');
      assert((await fetch('/post/s/browser-post', {credentials: 'omit'})).status === 404, 'Unpublished post remains public.');
      assert(document.querySelector('.post-heading').textContent.includes('Draft preview'), 'Unpublish did not restore the draft preview.');
      await savePublication(document.querySelector('.content-actions .action-edit'), true, 'republished');
      return;
    }
    if (stage === 'republished') {
      assert((await fetch('/post/s/browser-post', {credentials: 'omit'})).ok, 'Republished post is not public.');
      sessionStorage.setItem('blog-test-stage', 'logout');
      document.querySelector('form[action="/auth/logout"]').requestSubmit();
      return;
    }
    if (stage === 'logout') {
      const notice = document.querySelector('.notification--success');
      assert(notice?.textContent.includes('logged out'), 'Logout notification is missing.');
      await wait(() => !notice.isConnected);
      assert(!document.querySelector('#notifications').textContent.trim(), 'Logout overlay remains after dismissal.');
      await report('PASS');
    }
  } catch (error) { await report(error.message + '\n' + error.stack); }
})();
