// Select the enhanced navigation styles before the browser lays out the body.
// Without JavaScript, the ordinary navigation links remain available.
document.documentElement.classList.add('js');

// Mobile backgrounds use the full viewport. Reserve only the content width,
// using zero extra space on browsers with overlay scrollbars.
{
  const probe = document.createElement('div');
  probe.style.cssText = 'position:absolute;visibility:hidden;width:100px;height:100px;overflow:scroll';
  document.documentElement.append(probe);
  document.documentElement.style.setProperty('--scrollbar-width', `${probe.offsetWidth - probe.clientWidth}px`);
  probe.remove();
}
