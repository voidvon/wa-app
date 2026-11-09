import type { DashboardNotificationSettingsResponse } from '../proto/byte/v/forge/waapp/v1/dashboard';
import { getWaResponse, mutateWaResponse } from './wa-api';

const settingsPath = '/api/wa/notification-settings';

export function getWaNotificationSettings(signal?: AbortSignal) {
  return getWaResponse<DashboardNotificationSettingsResponse>(settingsPath, signal).then(requireSettings);
}

// Transport configuration uses a private form boundary; addresses and secrets are never projected.
export function saveWaNotificationSettings(fields: URLSearchParams) {
  const form = new URLSearchParams(fields);
  form.set('action', 'save');
  return submitNotificationForm(form).then(requireSettings);
}

export function testWaNotificationSettings() {
  return submitNotificationForm(new URLSearchParams({ action: 'test' })).then(requireSettings);
}

function submitNotificationForm(form: URLSearchParams) {
  return mutateWaResponse<DashboardNotificationSettingsResponse>(settingsPath, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8' },
    body: form.toString(),
  });
}

function requireSettings(response: DashboardNotificationSettingsResponse) {
  if (!response.settings) throw new Error('未能读取飞书通知配置，请重新打开后重试');
  return response.settings;
}
