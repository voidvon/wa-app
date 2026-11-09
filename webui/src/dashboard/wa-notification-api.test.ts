import { afterEach, describe, expect, it, vi } from 'vitest';
import { getWaNotificationSettings, saveWaNotificationSettings, testWaNotificationSettings } from './wa-notification-api';

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe('notification settings requests', () => {
  it('reads safe configuration flags and uses private form fields to update transport configuration', async () => {
    const fetch = vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify({ settings: { enabled: true, webhook_configured: true, signing_configured: false } }))));
    vi.stubGlobal('fetch', fetch);
    const settings = await getWaNotificationSettings();
    expect(settings).toEqual({ enabled: true, webhook_configured: true, signing_configured: false });
    expect(fetch.mock.calls[0][0]).toBe('/api/wa/notification-settings');
    const fields = new URLSearchParams({ enabled: 'on', webhook_url: 'https://open.feishu.cn/open-apis/bot/v2/hook/synthetic-token', signing_secret: 'synthetic-secret' });
    await saveWaNotificationSettings(fields);
    expect(fields.has('action')).toBe(false);
    const init = fetch.mock.calls[1][1] as RequestInit;
    expect(init).toMatchObject({ method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8' } });
    expect(new URLSearchParams(String(init.body)).get('action')).toBe('save');
    expect(new URLSearchParams(String(init.body)).get('signing_secret')).toBe('synthetic-secret');
    await testWaNotificationSettings();
    expect(new URLSearchParams(String(fetch.mock.calls[2][1].body)).toString()).toBe('action=test');
  });

  it('surfaces readable save and delivery errors and rejects incomplete responses', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { message: '签名校验失败' } })));
    vi.stubGlobal('fetch', fetch);
    await expect(testWaNotificationSettings()).rejects.toThrow('签名校验失败');
    fetch.mockResolvedValue(new Response('{}'));
    await expect(getWaNotificationSettings()).rejects.toThrow('未能读取飞书通知配置');
  });
});
