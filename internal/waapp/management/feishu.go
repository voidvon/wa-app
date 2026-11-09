package management

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

// FeishuConfig is service-local transport configuration, never an RPC model.
// It contains reusable secrets and must not appear in logs or API projections.
type FeishuConfig struct {
	Enabled       bool   `json:"enabled"`
	WebhookURL    string `json:"webhook_url"`
	SigningSecret string `json:"signing_secret"`
}

type Notifications struct {
	repository Repository
	client     *http.Client
	mu         sync.RWMutex
	config     FeishuConfig
	delivery   sync.Mutex
}

func NewNotifications(ctx context.Context, repository Repository) (*Notifications, error) {
	payload, err := repository.LoadPrivateConfiguration(ctx, "feishu")
	if err != nil {
		return nil, errors.New("load notification configuration failed")
	}
	var cfg FeishuConfig
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &cfg); err != nil {
			return nil, errors.New("invalid notification configuration")
		}
		if err := validateFeishuConfig(cfg); err != nil {
			return nil, err
		}
	}
	return &Notifications{repository: repository, config: cfg, client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (n *Notifications) Configuration() FeishuConfig {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.config
}

func (n *Notifications) SaveConfiguration(ctx context.Context, cfg FeishuConfig) error {
	cfg.WebhookURL = strings.TrimSpace(cfg.WebhookURL)
	cfg.SigningSecret = strings.TrimSpace(cfg.SigningSecret)
	if err := validateFeishuConfig(cfg); err != nil {
		return err
	}
	payload, err := json.Marshal(cfg)
	if err != nil {
		return errors.New("保存配置失败")
	}
	n.delivery.Lock()
	defer n.delivery.Unlock()
	if err := n.repository.SavePrivateConfiguration(ctx, "feishu", payload); err != nil {
		return errors.New("保存配置失败")
	}
	n.mu.Lock()
	n.config = cfg
	n.mu.Unlock()
	return nil
}

func validateFeishuConfig(cfg FeishuConfig) error {
	if cfg.WebhookURL == "" && !cfg.Enabled {
		return nil
	}
	target, err := url.Parse(cfg.WebhookURL)
	if err != nil || target.Scheme != "https" || target.User != nil || target.RawQuery != "" || target.Fragment != "" || (target.Host != "open.feishu.cn" && target.Host != "open.larksuite.com") {
		return errors.New("请填写飞书或 Lark 官方 HTTPS 机器人 Webhook 地址")
	}
	suffix, ok := strings.CutPrefix(target.Path, "/open-apis/bot/v2/hook/")
	if !ok || suffix == "" || strings.Contains(suffix, "/") || target.RawPath != "" || strings.ContainsAny(suffix, "\r\n ") {
		return errors.New("机器人 Webhook 地址格式不正确")
	}
	if len(cfg.SigningSecret) > 256 {
		return errors.New("签名密钥过长")
	}
	return nil
}

func (n *Notifications) Enqueue(ctx context.Context, session *waappv1.MessageSession, msg *waappv1.InboundMessage, decrypted *waappv1.DecryptedMessage) error {
	if !n.Configuration().Enabled {
		return nil
	}
	notification := BuildMessageNotification(session, msg, decrypted)
	if notification == nil {
		return nil
	}
	return n.repository.EnqueueNotification(ctx, notification)
}

func BuildMessageNotification(session *waappv1.MessageSession, msg *waappv1.InboundMessage, decrypted *waappv1.DecryptedMessage) *protocolv1.MessageNotification {
	if session.GetWaAccountId() == "" || msg.GetMessageId() == "" || msg.GetKind() != waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE || msg.GetDirection() == waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND || msg.GetDirection() == waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_SYSTEM || msg.GetSource() == waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_IMPORTED_HISTORY || msg.GetSource() == waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_LOCAL_SEND || decrypted.GetStatus() != waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED || decrypted.GetContent().GetType() == waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL {
		return nil
	}
	text := decrypted.GetPlaintextText().GetValue()
	if text == "" {
		text = "[非文本消息]"
	}
	id := msg.GetProviderMessageId()
	if id == "" {
		id = msg.GetMessageId()
	}
	sum := sha256.Sum256([]byte(session.GetWaAccountId() + "\x00" + msg.GetContactRef() + "\x00" + msg.GetSenderRef() + "\x00" + id))
	return &protocolv1.MessageNotification{NotificationId: hex.EncodeToString(sum[:]), WaAccountId: session.GetWaAccountId(), ContactRef: msg.GetContactRef(), SenderRef: msg.GetSenderRef(), Text: text, ReceivedAt: msg.GetReceivedAt()}
}

func (n *Notifications) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := n.deliverNext(ctx); err != nil && ctx.Err() == nil {
				log.Printf("feishu notification delivery failed; pending notification will be retried")
			}
		}
	}
}

func (n *Notifications) deliverNext(ctx context.Context) error {
	n.delivery.Lock()
	defer n.delivery.Unlock()
	cfg := n.Configuration()
	if !cfg.Enabled {
		return nil
	}
	items, err := n.repository.PendingNotifications(ctx, time.Now(), 1)
	if err != nil || len(items) == 0 {
		return err
	}
	item := items[0]
	if err := n.send(ctx, cfg, notificationText(item)); err != nil {
		if retryErr := n.repository.RetryNotification(ctx, item.GetNotificationId(), time.Now().Add(time.Minute)); retryErr != nil {
			return errors.New("persist notification retry failed")
		}
		return err
	}
	return n.repository.FinishNotification(ctx, item.GetNotificationId())
}

func (n *Notifications) Test(ctx context.Context) error {
	cfg := n.Configuration()
	if cfg.WebhookURL == "" {
		return errors.New("请先保存 Webhook 地址")
	}
	return n.send(ctx, cfg, "WA 消息通知：飞书 Webhook 测试成功")
}

func notificationText(item *protocolv1.MessageNotification) string {
	text := []rune(item.GetText())
	if len(text) > 3000 {
		text = append(text[:3000], []rune("\n[内容已截断]")...)
	}
	body := fmt.Sprintf("WA 新消息\n账号：%s\n会话：%s\n发送人：%s\n时间：%s\n\n%s", item.GetWaAccountId(), item.GetContactRef(), item.GetSenderRef(), item.GetReceivedAt().AsTime().Format(time.RFC3339), string(text))
	return strings.ReplaceAll(body, "<at", "＜at")
}

type feishuRequest struct {
	Timestamp   string `json:"timestamp,omitempty"`
	Sign        string `json:"sign,omitempty"`
	MessageType string `json:"msg_type"`
	Content     struct {
		Text string `json:"text"`
	} `json:"content"`
}

type feishuResponse struct {
	Code       *int `json:"code"`
	StatusCode *int `json:"StatusCode"`
}

func (n *Notifications) send(ctx context.Context, cfg FeishuConfig, text string) error {
	payload := feishuRequest{MessageType: "text"}
	payload.Content.Text = text
	if cfg.SigningSecret != "" {
		payload.Timestamp = strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(payload.Timestamp+"\n"+cfg.SigningSecret))
		payload.Sign = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.New("生成飞书消息失败")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return errors.New("飞书请求配置无效")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := n.client.Do(request)
	if err != nil {
		return errors.New("飞书请求失败，请检查网络和机器人配置")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("飞书返回 HTTP %d", response.StatusCode)
	}
	var result feishuResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 16384)).Decode(&result); err != nil {
		return errors.New("飞书响应无效")
	}
	if result.Code != nil && *result.Code != 0 {
		return fmt.Errorf("飞书拒绝消息，错误码 %d", *result.Code)
	}
	if result.StatusCode != nil && *result.StatusCode != 0 {
		return fmt.Errorf("飞书拒绝消息，错误码 %d", *result.StatusCode)
	}
	if result.Code == nil && result.StatusCode == nil {
		return errors.New("飞书响应缺少状态码")
	}
	return nil
}
