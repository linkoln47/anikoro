package usecase

import (
	"context"
	"errors"
	"testing"

	"test/internal/domain"
	"test/internal/ports"
)

type fakeAuthRepo struct {
	ports.AuthRepository

	createdEmail    string
	createdUsername string
	createdHash     string
	createUser      domain.User
	createErr       error

	credUser  domain.User
	credHash  string
	credFound bool
	credErr   error

	attachUserID int64
	attachUser   domain.User
	attachErr    error

	unlinkUserID int64
	unlinkUser   domain.User
	unlinkErr    error

	savedToken   domain.MALToken
	savedUserID  int64
	saveTokenErr error
}

func (repo *fakeAuthRepo) CreateUserWithPassword(ctx context.Context, email, username, passwordHash string) (domain.User, error) {
	repo.createdEmail = email
	repo.createdUsername = username
	repo.createdHash = passwordHash
	return repo.createUser, repo.createErr
}

func (repo *fakeAuthRepo) UserCredentialsByEmail(ctx context.Context, email string) (domain.User, string, bool, error) {
	return repo.credUser, repo.credHash, repo.credFound, repo.credErr
}

func (repo *fakeAuthRepo) AttachMALIdentity(ctx context.Context, userID int64, profile domain.MALUserProfile) (domain.User, error) {
	repo.attachUserID = userID
	return repo.attachUser, repo.attachErr
}

func (repo *fakeAuthRepo) UnlinkMALAccount(ctx context.Context, userID int64) (domain.User, error) {
	repo.unlinkUserID = userID
	return repo.unlinkUser, repo.unlinkErr
}

func (repo *fakeAuthRepo) SaveToken(ctx context.Context, userID int64, token domain.MALToken) error {
	repo.savedUserID = userID
	repo.savedToken = token
	return repo.saveTokenErr
}

// fakeHasher hashes by prefixing, so Compare succeeds only on the matching pair.
type fakeHasher struct {
	hashErr error
}

func (h *fakeHasher) Hash(plain string) (string, error) {
	if h.hashErr != nil {
		return "", h.hashErr
	}
	return "hashed:" + plain, nil
}

func (h *fakeHasher) Compare(hashed, plain string) error {
	if hashed == "hashed:"+plain {
		return nil
	}
	return ports.ErrPasswordMismatch
}

type fakeOAuthClient struct {
	token       *domain.MALToken
	tokenErr    error
	profile     domain.MALUserProfile
	profileErr  error
	gotCode     string
	gotVerifier string
}

func (c *fakeOAuthClient) ExchangeCodeForToken(ctx context.Context, config ports.MALOAuthConfig, code, verifier string) (*domain.MALToken, error) {
	c.gotCode = code
	c.gotVerifier = verifier
	return c.token, c.tokenErr
}

func (c *fakeOAuthClient) FetchCurrentUser(ctx context.Context, token string) (domain.MALUserProfile, error) {
	return c.profile, c.profileErr
}

func newAuthServiceForTest(repo ports.AuthRepository, hasher ports.PasswordHasher, oauth ports.MALOAuthClient) *AuthService {
	return NewAuthService(AuthServiceDependencies{
		Repo:   repo,
		Hasher: hasher,
		OAuth:  oauth,
	})
}

func TestRegisterHashesAndCreatesUser(t *testing.T) {
	repo := &fakeAuthRepo{createUser: domain.User{ID: 1, Username: "Alice", Email: "alice@example.com"}}
	service := newAuthServiceForTest(repo, &fakeHasher{}, &fakeOAuthClient{})

	user, err := service.Register(context.Background(), " Alice@Example.com ", "Alice", "supersecret")
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if user.ID != 1 {
		t.Fatalf("expected created user id 1, got %+v", user)
	}
	if repo.createdEmail != "alice@example.com" {
		t.Fatalf("expected normalized email, got %q", repo.createdEmail)
	}
	if repo.createdHash != "hashed:supersecret" {
		t.Fatalf("expected hashed password, got %q", repo.createdHash)
	}
}

func TestRegisterRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name     string
		email    string
		username string
		password string
		want     error
	}{
		{"bad email", "nope", "Alice", "supersecret", domain.ErrInvalidEmail},
		{"bad username", "alice@example.com", "a", "supersecret", domain.ErrInvalidUsername},
		{"weak password", "alice@example.com", "Alice", "short", domain.ErrWeakPassword},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeAuthRepo{}
			service := newAuthServiceForTest(repo, &fakeHasher{}, &fakeOAuthClient{})

			_, err := service.Register(context.Background(), tc.email, tc.username, tc.password)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Register error = %v, want %v", err, tc.want)
			}
			if repo.createdHash != "" {
				t.Fatal("invalid input must not reach the repository")
			}
		})
	}
}

func TestRegisterPassesConflictThrough(t *testing.T) {
	repo := &fakeAuthRepo{createErr: domain.ErrEmailTaken}
	service := newAuthServiceForTest(repo, &fakeHasher{}, &fakeOAuthClient{})

	_, err := service.Register(context.Background(), "alice@example.com", "Alice", "supersecret")
	if !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("Register error = %v, want ErrEmailTaken", err)
	}
}

func TestAuthenticateSuccess(t *testing.T) {
	repo := &fakeAuthRepo{
		credUser:  domain.User{ID: 7, Username: "Alice", Email: "alice@example.com"},
		credHash:  "hashed:supersecret",
		credFound: true,
	}
	service := newAuthServiceForTest(repo, &fakeHasher{}, &fakeOAuthClient{})

	user, err := service.Authenticate(context.Background(), "alice@example.com", "supersecret")
	if err != nil {
		t.Fatalf("Authenticate returned error: %v", err)
	}
	if user.ID != 7 {
		t.Fatalf("expected user id 7, got %+v", user)
	}
}

func TestAuthenticateInvalidCredentials(t *testing.T) {
	t.Run("unknown email", func(t *testing.T) {
		repo := &fakeAuthRepo{credFound: false}
		service := newAuthServiceForTest(repo, &fakeHasher{}, &fakeOAuthClient{})

		_, err := service.Authenticate(context.Background(), "ghost@example.com", "supersecret")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Authenticate error = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		repo := &fakeAuthRepo{
			credUser:  domain.User{ID: 7},
			credHash:  "hashed:supersecret",
			credFound: true,
		}
		service := newAuthServiceForTest(repo, &fakeHasher{}, &fakeOAuthClient{})

		_, err := service.Authenticate(context.Background(), "alice@example.com", "wrongpass")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Authenticate error = %v, want ErrInvalidCredentials", err)
		}
	})
}

func TestLinkMALAttachesAndSavesToken(t *testing.T) {
	repo := &fakeAuthRepo{attachUser: domain.User{ID: 7, Username: "Alice", MALUserID: 555}}
	oauth := &fakeOAuthClient{
		token:   &domain.MALToken{AccessToken: "access-1"},
		profile: domain.MALUserProfile{ID: 555, Username: "AliceMAL"},
	}
	service := newAuthServiceForTest(repo, &fakeHasher{}, oauth)

	user, err := service.LinkMAL(context.Background(), 7, "code-1", "verifier-1")
	if err != nil {
		t.Fatalf("LinkMAL returned error: %v", err)
	}
	if user.MALUserID != 555 {
		t.Fatalf("expected linked mal_user_id 555, got %+v", user)
	}
	if repo.attachUserID != 7 {
		t.Fatalf("expected attach to user 7, got %d", repo.attachUserID)
	}
	if repo.savedUserID != 7 || repo.savedToken.AccessToken != "access-1" {
		t.Fatalf("expected token saved for user 7, got user=%d token=%q", repo.savedUserID, repo.savedToken.AccessToken)
	}
}

func TestUnlinkMALDelegatesToRepo(t *testing.T) {
	repo := &fakeAuthRepo{unlinkUser: domain.User{ID: 7, Username: "Alice", Email: "alice@example.com"}}
	service := newAuthServiceForTest(repo, &fakeHasher{}, &fakeOAuthClient{})

	user, err := service.UnlinkMAL(context.Background(), 7)
	if err != nil {
		t.Fatalf("UnlinkMAL returned error: %v", err)
	}
	if repo.unlinkUserID != 7 {
		t.Fatalf("expected unlink for user 7, got %d", repo.unlinkUserID)
	}
	if user.MALLinked() {
		t.Fatalf("expected unlinked user, got %+v", user)
	}
}

func TestUnlinkMALRejectsInvalidUser(t *testing.T) {
	service := newAuthServiceForTest(&fakeAuthRepo{}, &fakeHasher{}, &fakeOAuthClient{})

	if _, err := service.UnlinkMAL(context.Background(), 0); err == nil {
		t.Fatal("expected error for non-positive user id")
	}
}

func TestLinkMALPassesConflictThrough(t *testing.T) {
	repo := &fakeAuthRepo{attachErr: domain.ErrMALAlreadyLinked}
	oauth := &fakeOAuthClient{
		token:   &domain.MALToken{AccessToken: "access-1"},
		profile: domain.MALUserProfile{ID: 555, Username: "AliceMAL"},
	}
	service := newAuthServiceForTest(repo, &fakeHasher{}, oauth)

	_, err := service.LinkMAL(context.Background(), 7, "code-1", "verifier-1")
	if !errors.Is(err, domain.ErrMALAlreadyLinked) {
		t.Fatalf("LinkMAL error = %v, want ErrMALAlreadyLinked", err)
	}
	if repo.savedToken.AccessToken != "" {
		t.Fatal("token must not be saved when linking conflicts")
	}
}
