import { type FormEvent, useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Bell, Loader2, Send } from 'lucide-react';
import { Switch } from 'radix-ui';
import { toast } from 'sonner';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Field, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import type { DashboardNotificationSettings } from '../proto/byte/v/forge/waapp/v1/dashboard';
import { getWaNotificationSettings, saveWaNotificationSettings, testWaNotificationSettings } from './wa-notification-api';

const settingsKey = ['wa', 'notification-settings'] as const;

export function WaNotificationSettings({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg">
        <DialogHeader className="pr-7">
          <DialogTitle><Bell size={16} />飞书消息通知</DialogTitle>
          <DialogDescription>将所有账号收到的新消息推送到飞书群。</DialogDescription>
        </DialogHeader>
        <NotificationSettingsContent />
      </DialogContent>
    </Dialog>
  );
}

function NotificationSettingsContent() {
  const query = useQuery({ queryKey: settingsKey, queryFn: ({ signal }) => getWaNotificationSettings(signal), staleTime: 0, refetchOnWindowFocus: false });
  if (query.isPending) return <p role="status" className="flex items-center gap-2 py-6 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />正在读取通知配置…</p>;
  if (query.isError) return <div className="grid gap-4"><Alert variant="destructive" role="alert"><AlertDescription>{query.error.message}</AlertDescription></Alert><DialogFooter><DialogClose asChild><Button type="button" variant="outline">关闭</Button></DialogClose><Button type="button" onClick={() => void query.refetch()}>重新加载</Button></DialogFooter></div>;
  return <NotificationSettingsForm settings={query.data} />;
}

function NotificationSettingsForm({ settings }: { settings: DashboardNotificationSettings }) {
  const id = useId();
  const queryClient = useQueryClient();
  const [enabled, setEnabled] = useState(Boolean(settings.enabled));
  const [webhook, setWebhook] = useState('');
  const [secret, setSecret] = useState('');
  const [removeSecret, setRemoveSecret] = useState(false);
  const saving = useMutation({
    gcTime: 0,
    mutationFn: () => {
      const form = new URLSearchParams({ webhook_url: webhook.trim(), signing_secret: secret.trim() });
      if (enabled) form.set('enabled', 'on');
      if (removeSecret) form.set('remove_secret', 'on');
      return saveWaNotificationSettings(form);
    },
    onSuccess: (saved) => {
      queryClient.setQueryData(settingsKey, saved);
      setEnabled(Boolean(saved.enabled));
      setWebhook('');
      setSecret('');
      setRemoveSecret(false);
      toast.success('飞书通知配置已保存');
    },
  });
  const testing = useMutation({ mutationFn: testWaNotificationSettings, onSuccess: () => toast.success('测试消息已发送，请到飞书群查看') });
  const busy = saving.isPending || testing.isPending;
  const dirty = enabled !== Boolean(settings.enabled) || Boolean(webhook.trim() || secret.trim()) || removeSecret;
  const error = saving.error || testing.error;
  function submit(event: FormEvent) { event.preventDefault(); testing.reset(); saving.mutate(); }
  return (
    <form className="grid gap-5" onSubmit={submit}>
      <fieldset className="grid gap-5" disabled={busy}>
        <div className="flex items-center justify-between gap-4 rounded-xl border border-border p-4">
          <div className="grid gap-1"><FieldLabel htmlFor={`${id}-enabled`}>启用新消息通知</FieldLabel><p className="text-xs text-muted-foreground">成功解密后自动发送，失败时重试。</p></div>
          <Switch.Root id={`${id}-enabled`} checked={enabled} onCheckedChange={setEnabled} disabled={busy} className="h-6 w-10 shrink-0 rounded-full bg-muted-foreground/30 p-0.5 transition-colors outline-offset-2 data-[state=checked]:bg-primary disabled:opacity-50"><Switch.Thumb className="block size-5 rounded-full bg-white shadow-sm transition-transform data-[state=checked]:translate-x-4" /></Switch.Root>
        </div>
        <Field>
          <div className="flex items-center justify-between gap-2"><FieldLabel htmlFor={`${id}-webhook`}>机器人 Webhook</FieldLabel>{settings.webhook_configured && <Badge variant="secondary">已配置</Badge>}</div>
          <Input id={`${id}-webhook`} type="password" autoComplete="off" maxLength={2048} placeholder={settings.webhook_configured ? '留空保持当前地址' : '粘贴飞书机器人 Webhook 地址'} value={webhook} onChange={(event) => setWebhook(event.target.value)} aria-describedby={`${id}-webhook-hint`} />
          <p id={`${id}-webhook-hint`} className="text-xs leading-5 text-muted-foreground">支持飞书与 Lark 官方机器人地址，保存后不回显。</p>
        </Field>
        <Field>
          <div className="flex items-center justify-between gap-2"><FieldLabel htmlFor={`${id}-secret`}>签名密钥（可选）</FieldLabel>{settings.signing_configured && <Badge variant="secondary">已配置</Badge>}</div>
          <Input id={`${id}-secret`} type="password" autoComplete="new-password" maxLength={256} placeholder={settings.signing_configured ? '留空保持当前密钥' : '机器人开启签名校验时填写'} disabled={removeSecret || busy} value={secret} onChange={(event) => setSecret(event.target.value)} aria-describedby={`${id}-secret-hint`} />
          <p id={`${id}-secret-hint`} className="text-xs leading-5 text-muted-foreground">在机器人的安全设置中获取；使用关键词校验时请包含“WA”。</p>
          {settings.signing_configured && <label className="flex items-center gap-2 text-xs text-muted-foreground"><input type="checkbox" checked={removeSecret} onChange={(event) => { setRemoveSecret(event.target.checked); setSecret(''); }} className="size-4 accent-primary" />清除已保存的签名密钥</label>}
        </Field>
      </fieldset>
      <p className="text-xs leading-5 text-muted-foreground">推送账号、会话、发送人和消息正文，媒体使用文字摘要。历史消息、自己发送的消息和回执不会推送。关闭通知会暂停发送，重新开启后继续处理待发消息。</p>
      {error && <Alert variant="destructive" role="alert"><AlertDescription>{error.message}</AlertDescription></Alert>}
      <div className="grid gap-2">
        <p className="text-xs text-muted-foreground">{dirty ? '请先保存修改，再发送测试消息。' : '测试使用已保存的配置，消息将发送到飞书群。'}</p>
        <DialogFooter className="flex-wrap">
          <Button type="button" variant="outline" className="mr-auto" disabled={busy || dirty || !settings.webhook_configured} onClick={() => { saving.reset(); testing.mutate(); }}>{testing.isPending ? <Loader2 className="size-4 animate-spin" /> : <Send className="size-4" />}发送测试</Button>
          <DialogClose asChild><Button type="button" variant="outline">关闭</Button></DialogClose>
          <Button type="submit" disabled={busy}>{saving.isPending && <Loader2 className="size-4 animate-spin" />}{saving.isPending ? '正在保存…' : '保存配置'}</Button>
        </DialogFooter>
      </div>
    </form>
  );
}
