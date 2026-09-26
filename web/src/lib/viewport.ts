// Keep the composer above keyboards that resize only the visual viewport (iOS).
// Ignore pinch zoom so the page remains freely zoomable.
export function trackViewport() {
  const viewport = window.visualViewport;
  const update = () => {
    if (viewport && viewport.scale !== 1) return;
    const height = viewport?.height ?? window.innerHeight;
    document.documentElement.style.setProperty('--app-height', `${height}px`);
    document.documentElement.style.setProperty('--app-top', `${viewport?.offsetTop ?? 0}px`);
  };
  update();
  viewport?.addEventListener('resize', update);
  viewport?.addEventListener('scroll', update);
  window.addEventListener('resize', update);
  return () => {
    viewport?.removeEventListener('resize', update);
    viewport?.removeEventListener('scroll', update);
    window.removeEventListener('resize', update);
    document.documentElement.style.removeProperty('--app-height');
    document.documentElement.style.removeProperty('--app-top');
  };
}
