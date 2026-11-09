const activityPath = '/api/dashboard/session/activity';
const activityInterval = 60_000;

// Polling never calls this endpoint. Report visible user interactions at most
// once a minute so an unattended dashboard can still reach its idle deadline.
export function trackDashboardSessionActivity() {
  let lastAttempt = -Infinity;
  let pending: AbortController | undefined;
  const events = ['pointerdown', 'keydown', 'wheel', 'touchstart'] as const;

  function report(event?: Event) {
    if (event && !event.isTrusted) return;
    if (document.visibilityState !== 'visible' || !document.hasFocus() || pending) return;
    if (Date.now() - lastAttempt < activityInterval) return;
    lastAttempt = Date.now();
    const controller = new AbortController();
    pending = controller;
    void fetch(activityPath, {
      method: 'POST',
      credentials: 'same-origin',
      signal: AbortSignal.any([controller.signal, AbortSignal.timeout(10_000)]),
    }).then((response) => {
      if (response.status === 401 && !controller.signal.aborted) {
        const next = `${window.location.pathname}${window.location.search}${window.location.hash}`;
        window.location.assign(`/login?next=${encodeURIComponent(next || '/')}`);
      }
    }).catch(() => {
      // A failed activity report does not extend the server deadline. A later
      // interaction can retry; regular API requests surface connectivity errors.
    }).finally(() => { pending = undefined; });
  }

  for (const event of events) document.addEventListener(event, report, { passive: true });
  document.addEventListener('visibilitychange', report);
  window.addEventListener('focus', report);
  report();
  return () => {
    pending?.abort();
    for (const event of events) document.removeEventListener(event, report);
    document.removeEventListener('visibilitychange', report);
    window.removeEventListener('focus', report);
  };
}
