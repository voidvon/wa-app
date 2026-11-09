package management

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/timestamppb"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

var (
	ErrAlreadyInitialized = errors.New("administrator already exists")
	ErrInvalidCredentials = errors.New("invalid username or password")
)

const (
	SessionIdleDuration           = 30 * time.Minute
	SessionMaxDuration            = 8 * time.Hour
	RememberedSessionIdleDuration = 7 * 24 * time.Hour
	RememberedSessionMaxDuration  = 30 * 24 * time.Hour
)

type Repository interface {
	GetAdministrator(context.Context) (*protocolv1.DashboardAdministrator, error)
	CreateAdministrator(context.Context, *protocolv1.DashboardAdministrator) (bool, error)
	SaveDashboardSession(context.Context, *protocolv1.DashboardSession) error
	GetDashboardSession(context.Context, string, time.Time) (*protocolv1.DashboardSession, error)
	RenewDashboardSession(context.Context, *protocolv1.DashboardSession, time.Time) (*protocolv1.DashboardSession, error)
	DeleteDashboardSession(context.Context, string) error
	LoadPrivateConfiguration(context.Context, string) ([]byte, error)
	SavePrivateConfiguration(context.Context, string, []byte) error
	EnqueueNotification(context.Context, *protocolv1.MessageNotification) error
	PendingNotifications(context.Context, time.Time, int) ([]*protocolv1.MessageNotification, error)
	FinishNotification(context.Context, string) error
	RetryNotification(context.Context, string, time.Time) error
}

type Administrator struct {
	repository Repository
	dummyHash  []byte
	now        func() time.Time
}

func NewAdministrator(repository Repository) (*Administrator, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte("synthetic-invalid-credential"), 12)
	if err != nil {
		return nil, err
	}
	return &Administrator{repository: repository, dummyHash: hash, now: time.Now}, nil
}

func (a *Administrator) Initialized(ctx context.Context) (bool, error) {
	admin, err := a.repository.GetAdministrator(ctx)
	return admin != nil, err
}

func (a *Administrator) Setup(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 || strings.ContainsFunc(username, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
		return errors.New("用户名需为 3–64 字节，不能包含空格或控制字符")
	}
	if len(password) < 12 || len(password) > 72 || strings.TrimSpace(password) == "" {
		return errors.New("密码需为 12–72 字节")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return errors.New("创建管理员失败")
	}
	created, err := a.repository.CreateAdministrator(ctx, &protocolv1.DashboardAdministrator{Username: username, PasswordHash: hash, CreatedAt: timestamppb.Now()})
	if err != nil {
		return errors.New("保存管理员失败")
	}
	if !created {
		return ErrAlreadyInitialized
	}
	return nil
}

func (a *Administrator) Login(ctx context.Context, username, password string, rememberDevice bool) (string, *protocolv1.DashboardSession, error) {
	admin, err := a.repository.GetAdministrator(ctx)
	if err != nil {
		return "", nil, errors.New("读取管理员失败")
	}
	hash := a.dummyHash
	if admin != nil {
		hash = admin.GetPasswordHash()
	}
	validPassword := bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
	if !validPassword || admin == nil || username != admin.GetUsername() {
		return "", nil, ErrInvalidCredentials
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", nil, errors.New("创建登录会话失败")
	}
	token := hex.EncodeToString(secret[:])
	idle, maximum := SessionIdleDuration, SessionMaxDuration
	if rememberDevice {
		idle, maximum = RememberedSessionIdleDuration, RememberedSessionMaxDuration
	}
	now := a.now()
	session := &protocolv1.DashboardSession{TokenHash: sessionHash(token), ExpiresAt: timestamppb.New(now.Add(idle)), AbsoluteExpiresAt: timestamppb.New(now.Add(maximum)), RememberDevice: rememberDevice}
	if err := a.repository.SaveDashboardSession(ctx, session); err != nil {
		return "", nil, errors.New("保存登录会话失败")
	}
	return token, session, nil
}

func (a *Administrator) Authenticated(ctx context.Context, token string) bool {
	return a.Authenticate(ctx, token, false) != nil
}

// Authenticate only extends an unexpired session on explicit user activity.
func (a *Administrator) Authenticate(ctx context.Context, token string, activity bool) *protocolv1.DashboardSession {
	if len(token) != 64 {
		return nil
	}
	now := a.now()
	session, err := a.repository.GetDashboardSession(ctx, sessionHash(token), now)
	if err != nil || session == nil {
		return nil
	}
	if !now.Before(session.GetExpiresAt().AsTime()) || !now.Before(session.GetAbsoluteExpiresAt().AsTime()) {
		return nil
	}
	if !activity {
		return session
	}
	idle := SessionIdleDuration
	if session.GetRememberDevice() {
		idle = RememberedSessionIdleDuration
	}
	expires := now.Add(idle)
	if absolute := session.GetAbsoluteExpiresAt().AsTime(); expires.After(absolute) {
		expires = absolute
	}
	session.ExpiresAt = timestamppb.New(expires)
	renewed, err := a.repository.RenewDashboardSession(ctx, session, now)
	if err != nil {
		return nil
	}
	return renewed
}

func (a *Administrator) Logout(ctx context.Context, token string) error {
	return a.repository.DeleteDashboardSession(ctx, sessionHash(token))
}

func sessionHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
