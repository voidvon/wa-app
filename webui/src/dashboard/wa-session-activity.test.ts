import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { trackDashboardSessionActivity } from './wa-session-activity';

let page: EventTarget & { visibilityState: string; hasFocus: () => boolean };
let browser: EventTarget & { location: { pathname: string; search: string; hash: string; assign: ReturnType<typeof vi.fn> } };
let stop: (() => void) | undefined;

beforeEach(() => {
  vi.useFakeTimers();
  page = Object.assign(new EventTarget(), { visibilityState: 'visible', hasFocus: () => true });
  browser = Object.assign(new EventTarget(), { location: { pathname: '/accounts/fixture/chats', search: '?view=unread', hash: '', assign: vi.fn() } });
  vi.stubGlobal('document', page);
  vi.stubGlobal('window', browser);
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 204 })));
});

afterEach(() => {
  stop?.();
  stop = undefined;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function interact(type = 'pointerdown', target: EventTarget = page) {
  const event = new Event(type);
  Object.defineProperty(event, 'isTrusted', { value: true });
  target.dispatchEvent(event);
}

describe('dashboard session activity', () => {
  it('reports opening the page and throttles actual interaction without a heartbeat', async () => {
    stop = trackDashboardSessionActivity();
    await vi.advanceTimersByTimeAsync(0);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch).toHaveBeenCalledWith('/api/dashboard/session/activity', expect.objectContaining({ method: 'POST', credentials: 'same-origin' }));
    interact();
    expect(fetch).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(30 * 60_000);
    expect(fetch).toHaveBeenCalledTimes(1);
    interact('keydown');
    await vi.advanceTimersByTimeAsync(0);
    expect(fetch).toHaveBeenCalledTimes(2);
    stop();
    await vi.advanceTimersByTimeAsync(60_000);
    interact('wheel');
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('ignores synthetic events, hidden pages, and unfocused windows', async () => {
    page.visibilityState = 'hidden';
    stop = trackDashboardSessionActivity();
    interact();
    expect(fetch).not.toHaveBeenCalled();
    page.visibilityState = 'visible';
    page.hasFocus = () => false;
    interact();
    expect(fetch).not.toHaveBeenCalled();
    page.hasFocus = () => true;
    page.dispatchEvent(new Event('pointerdown'));
    expect(fetch).not.toHaveBeenCalled();
    interact('focus', browser);
    await vi.advanceTimersByTimeAsync(0);
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('does not overlap requests and cancels activity when disposed', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => undefined)));
    stop = trackDashboardSessionActivity();
    vi.advanceTimersByTime(60_000);
    interact();
    expect(fetch).toHaveBeenCalledTimes(1);
    const signal = vi.mocked(fetch).mock.calls[0][1]?.signal;
    stop();
    expect(signal?.aborted).toBe(true);
  });

  it('requires login again when the server rejects an expired session', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 401 })));
    stop = trackDashboardSessionActivity();
    await vi.advanceTimersByTimeAsync(0);
    expect(browser.location.assign).toHaveBeenCalledWith('/login?next=%2Faccounts%2Ffixture%2Fchats%3Fview%3Dunread');
  });

  it('allows a later user interaction to retry after a network failure', async () => {
    const fetch = vi.fn().mockRejectedValueOnce(new TypeError('offline')).mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal('fetch', fetch);
    stop = trackDashboardSessionActivity();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(fetch).toHaveBeenCalledTimes(1);
    interact('touchstart');
    await vi.advanceTimersByTimeAsync(0);
    expect(fetch).toHaveBeenCalledTimes(2);
  });
});
