package management

import (
	"context"
	"testing"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/proto"
)

type administratorRepository struct {
	Repository
	admin   *protocolv1.DashboardAdministrator
	session *protocolv1.DashboardSession
}

func (r *administratorRepository) GetAdministrator(context.Context) (*protocolv1.DashboardAdministrator, error) {
	return r.admin, nil
}

func (r *administratorRepository) SaveDashboardSession(_ context.Context, session *protocolv1.DashboardSession) error {
	r.session = proto.Clone(session).(*protocolv1.DashboardSession)
	return nil
}

func (r *administratorRepository) GetDashboardSession(_ context.Context, hash string, _ time.Time) (*protocolv1.DashboardSession, error) {
	if r.session == nil || hash != r.session.GetTokenHash() {
		return nil, nil
	}
	return proto.Clone(r.session).(*protocolv1.DashboardSession), nil
}

func (r *administratorRepository) RenewDashboardSession(ctx context.Context, session *protocolv1.DashboardSession, _ time.Time) (*protocolv1.DashboardSession, error) {
	return session, r.SaveDashboardSession(ctx, session)
}

func TestAdministratorSessionDeadlines(t *testing.T) {
	for _, tc := range []struct {
		name          string
		remember      bool
		idle, maximum time.Duration
	}{
		{"ordinary", false, 30 * time.Minute, 8 * time.Hour},
		{"remembered", true, 7 * 24 * time.Hour, 30 * 24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := bcrypt.GenerateFromPassword([]byte("synthetic-password"), bcrypt.MinCost)
			if err != nil {
				t.Fatal(err)
			}
			start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			now := start
			repo := &administratorRepository{admin: &protocolv1.DashboardAdministrator{Username: "fixture-admin", PasswordHash: hash}}
			admin := &Administrator{repository: repo, dummyHash: hash, now: func() time.Time { return now }}
			if _, _, err := admin.Login(t.Context(), "fixture-admin", "wrong-password", tc.remember); err != ErrInvalidCredentials {
				t.Fatal("invalid password accepted")
			}
			token, session, err := admin.Login(t.Context(), "fixture-admin", "synthetic-password", tc.remember)
			if err != nil {
				t.Fatal(err)
			}
			if len(token) != 64 || session.GetTokenHash() == token || session.GetRememberDevice() != tc.remember {
				t.Fatal("invalid session material or mode")
			}
			if !session.GetExpiresAt().AsTime().Equal(start.Add(tc.idle)) || !session.GetAbsoluteExpiresAt().AsTime().Equal(start.Add(tc.maximum)) {
				t.Fatal("incorrect initial deadlines")
			}
			initial := proto.Clone(repo.session)
			now = start.Add(tc.idle - time.Second)
			if !admin.Authenticated(t.Context(), token) || !proto.Equal(initial, repo.session) {
				t.Fatal("polling changed the idle deadline")
			}
			now = start.Add(tc.idle)
			if admin.Authenticate(t.Context(), token, true) != nil {
				t.Fatal("idle expiry was revived by activity")
			}
			now = start
			for offset := tc.idle / 2; offset < tc.maximum; offset += tc.idle / 2 {
				now = start.Add(offset)
				renewed := admin.Authenticate(t.Context(), token, true)
				if renewed == nil {
					t.Fatal("active session expired early")
				}
				want := now.Add(tc.idle)
				if want.After(start.Add(tc.maximum)) {
					want = start.Add(tc.maximum)
				}
				if !renewed.GetExpiresAt().AsTime().Equal(want) || !renewed.GetAbsoluteExpiresAt().AsTime().Equal(start.Add(tc.maximum)) {
					t.Fatal("renewal exceeded its fixed maximum")
				}
			}
			now = start.Add(tc.maximum)
			if admin.Authenticated(t.Context(), token) || admin.Authenticate(t.Context(), token, true) != nil {
				t.Fatal("absolute expiry was bypassed")
			}
		})
	}
}
