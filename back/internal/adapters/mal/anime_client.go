package mal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"test/internal/domain"
	"test/internal/ports"
)

const (
	malAnimeListURL             = "https://api.myanimelist.net/v2/users/@me/animelist"
	malPublicAnimeListURLFormat = "https://api.myanimelist.net/v2/users/%s/animelist"
)

var errTransientAnimeDetails = errors.New("transient anime details error")

var (
	animeDetailsMaxAttempts      = 4
	animeDetailsNetworkRetryBase = 500 * time.Millisecond
	animeDetailsStatusRetryBase  = 700 * time.Millisecond
	animeDetailsPrimaryTimeout   = 3 * time.Second
	animeDetailsRetryTimeout     = 25 * time.Second
)

type animeListResponse struct {
	Data []struct {
		Node struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
		} `json:"node"`
		ListStatus struct {
			Status             string `json:"status"`
			Score              int    `json:"score"`
			NumEpisodesWatched int    `json:"num_episodes_watched"`
		} `json:"list_status"`
	} `json:"data"`
	Paging struct {
		Next string `json:"next"`
	} `json:"paging"`
}

type animeDetailsResponse struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	MediaType   string `json:"media_type"`
	StartDate   string `json:"start_date"`
	MainPicture struct {
		Medium string `json:"medium"`
		Large  string `json:"large"`
	} `json:"main_picture"`
	RelatedAnime []struct {
		Node struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
		} `json:"node"`
		RelationType          string `json:"relation_type"`
		RelationTypeFormatted string `json:"relation_type_formatted"`
	} `json:"related_anime"`
}

type animeDetailsRequestPlan struct {
	MaxAttempts      int
	NetworkRetryBase time.Duration
	Queue            string
	RequestTimeout   time.Duration
	StatusRetryBase  time.Duration
}

type MyAnimeListClient struct {
	httpClient *http.Client
	clientID   string
	logger     ports.SyncLogger
}

var _ ports.MALAnimeClient = (*MyAnimeListClient)(nil)

type apiAuth struct {
	bearerToken string
	clientID    string
}

func NewAnimeClient(httpClient *http.Client, clientID string, logger ports.SyncLogger) *MyAnimeListClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &MyAnimeListClient{
		httpClient: httpClient,
		clientID:   strings.TrimSpace(clientID),
		logger:     logger,
	}
}

func applyAPIAuth(req *http.Request, auth apiAuth) error {
	if auth.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+auth.bearerToken)
		return nil
	}
	if auth.clientID != "" {
		req.Header.Set("X-MAL-CLIENT-ID", auth.clientID)
		return nil
	}
	return errors.New("MAL API authorization is required")
}

func bearerAuth(token string) apiAuth {
	return apiAuth{bearerToken: strings.TrimSpace(token)}
}

func clientIDAuth(clientID string) apiAuth {
	return apiAuth{clientID: strings.TrimSpace(clientID)}
}

func (client *MyAnimeListClient) FetchAnimeList(ctx context.Context, token string) ([]domain.UserAnimeListEntry, error) {
	return client.fetchAnimeListEntriesWithAuthContext(ctx, malAnimeListURL, bearerAuth(token))
}

func (client *MyAnimeListClient) FetchPublicAnimeList(ctx context.Context, username string) ([]domain.UserAnimeListEntry, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, errors.New("MAL username is required")
	}

	listURL := fmt.Sprintf(malPublicAnimeListURLFormat, url.PathEscape(username))
	return client.fetchAnimeListEntriesWithAuthContext(ctx, listURL, clientIDAuth(client.clientID))
}

func (client *MyAnimeListClient) FetchAnimeDetails(
	ctx context.Context,
	token string,
	animeID int,
	cache ports.AnimeDetailsCacheStore,
	mode ports.AnimeDetailsFetchMode,
) (domain.AnimeDetails, error) {
	if mode == ports.AnimeDetailsFetchRetry {
		return client.fetchAnimeDetailsRetryWithAuthContext(ctx, bearerAuth(token), animeID)
	}
	return client.fetchAnimeDetailsPrimaryWithAuthContext(ctx, bearerAuth(token), animeID, cache)
}

func (client *MyAnimeListClient) FetchPublicAnimeDetails(
	ctx context.Context,
	animeID int,
	cache ports.AnimeDetailsCacheStore,
	mode ports.AnimeDetailsFetchMode,
) (domain.AnimeDetails, error) {
	if mode == ports.AnimeDetailsFetchRetry {
		return client.fetchAnimeDetailsRetryWithAuthContext(ctx, clientIDAuth(client.clientID), animeID)
	}
	return client.fetchAnimeDetailsPrimaryWithAuthContext(ctx, clientIDAuth(client.clientID), animeID, cache)
}

func (client *MyAnimeListClient) fetchAnimeListEntriesWithAuthContext(ctx context.Context, listURL string, auth apiAuth) ([]domain.UserAnimeListEntry, error) {
	ctx = ensureContext(ctx)

	u, err := url.Parse(listURL)
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("limit", "100")
	q.Set("fields", "list_status")
	u.RawQuery = q.Encode()

	allEntries := make([]domain.UserAnimeListEntry, 0)
	nextURL := u.String()
	page := 1
	for nextURL != "" {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		client.debug("mal_client", "requesting animelist page", "page", page, "url", nextURL)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, err
		}
		if err := applyAPIAuth(req, auth); err != nil {
			return nil, err
		}

		resp, err := client.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("MAL API returned %d: %s", resp.StatusCode, string(body))
		}

		var parsed animeListResponse
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		for _, item := range parsed.Data {
			listStatus, ok := domain.NormalizeAnimeListStatus(item.ListStatus.Status)
			if !ok {
				return nil, fmt.Errorf("MAL anime %d has unsupported list status %q", item.Node.ID, item.ListStatus.Status)
			}

			allEntries = append(allEntries, domain.UserAnimeListEntry{
				ID:                 item.Node.ID,
				Title:              item.Node.Title,
				Score:              item.ListStatus.Score,
				NumEpisodesWatched: item.ListStatus.NumEpisodesWatched,
				ListStatus:         listStatus,
			})
		}

		client.debug("mal_client", "received animelist page", "page", page, "entries", len(parsed.Data))
		nextURL = parsed.Paging.Next
		page++
	}

	return allEntries, nil
}

func (client *MyAnimeListClient) fetchAnimeDetailsPrimary(token string, animeID int, cache ports.AnimeDetailsCacheStore) (domain.AnimeDetails, error) {
	return client.fetchAnimeDetailsPrimaryWithContext(context.Background(), token, animeID, cache)
}

func (client *MyAnimeListClient) fetchAnimeDetailsPrimaryWithContext(ctx context.Context, token string, animeID int, cache ports.AnimeDetailsCacheStore) (domain.AnimeDetails, error) {
	return client.fetchAnimeDetailsPrimaryWithAuthContext(ctx, bearerAuth(token), animeID, cache)
}

func (client *MyAnimeListClient) fetchAnimeDetailsPrimaryWithAuthContext(ctx context.Context, auth apiAuth, animeID int, cache ports.AnimeDetailsCacheStore) (domain.AnimeDetails, error) {
	ctx = ensureContext(ctx)

	if err := ctx.Err(); err != nil {
		return domain.AnimeDetails{}, err
	}
	if animeID == 0 {
		return domain.AnimeDetails{}, nil
	}

	cached, ok := cache.Lookup(animeID)
	switch {
	case ok && cached.IsFresh(time.Now()):
		client.debug("mal_client", "anime details cache hit", "id", animeID)
		details := domain.CloneAnimeDetails(cached.Details)
		details.ID = animeID
		return details, nil
	case ok && cached.IsUsable():
		client.debug("mal_client", "anime details cache stale, refreshing", "id", animeID)
	case ok:
		client.debug("mal_client", "anime details cache unusable, refreshing", "id", animeID)
	default:
		client.debug("mal_client", "anime details cache miss", "id", animeID)
	}

	details, err := client.requestAnimeDetailsWithPlanAndAuthContext(ctx, auth, animeID, animeDetailsRequestPlan{
		MaxAttempts:      1,
		NetworkRetryBase: animeDetailsNetworkRetryBase,
		Queue:            "primary",
		RequestTimeout:   animeDetailsPrimaryTimeout,
		StatusRetryBase:  animeDetailsStatusRetryBase,
	})
	if err != nil {
		if errors.Is(err, errTransientAnimeDetails) && ok && cached.IsUsable() {
			client.warn("mal_client", "using stale cache after transient MAL error", "id", animeID, "err", err)
			details := domain.CloneAnimeDetails(cached.Details)
			details.ID = animeID
			return details, nil
		}
		return domain.AnimeDetails{}, err
	}

	if err := cache.StoreResolved(animeID, details); err != nil {
		client.warn("cache", "cannot flush details cache batch", "id", animeID, "err", err)
	}
	client.debug("mal_client", "anime details cache updated", "id", animeID)
	return details, nil
}

func (client *MyAnimeListClient) fetchAnimeDetailsRetry(token string, animeID int) (domain.AnimeDetails, error) {
	return client.fetchAnimeDetailsRetryWithContext(context.Background(), token, animeID)
}

func (client *MyAnimeListClient) fetchAnimeDetailsRetryWithContext(ctx context.Context, token string, animeID int) (domain.AnimeDetails, error) {
	return client.fetchAnimeDetailsRetryWithAuthContext(ctx, bearerAuth(token), animeID)
}

func (client *MyAnimeListClient) fetchAnimeDetailsRetryWithAuthContext(ctx context.Context, auth apiAuth, animeID int) (domain.AnimeDetails, error) {
	return client.requestAnimeDetailsWithPlanAndAuthContext(ctx, auth, animeID, animeDetailsRequestPlan{
		MaxAttempts:      animeDetailsMaxAttempts,
		NetworkRetryBase: animeDetailsNetworkRetryBase,
		Queue:            "retry",
		RequestTimeout:   animeDetailsRetryTimeout,
		StatusRetryBase:  animeDetailsStatusRetryBase,
	})
}

func (client *MyAnimeListClient) requestAnimeDetailsWithPlan(token string, animeID int, plan animeDetailsRequestPlan) (domain.AnimeDetails, error) {
	return client.requestAnimeDetailsWithPlanAndContext(context.Background(), token, animeID, plan)
}

func (client *MyAnimeListClient) requestAnimeDetailsWithPlanAndContext(ctx context.Context, token string, animeID int, plan animeDetailsRequestPlan) (domain.AnimeDetails, error) {
	return client.requestAnimeDetailsWithPlanAndAuthContext(ctx, bearerAuth(token), animeID, plan)
}

func (client *MyAnimeListClient) requestAnimeDetailsWithPlanAndAuthContext(ctx context.Context, auth apiAuth, animeID int, plan animeDetailsRequestPlan) (domain.AnimeDetails, error) {
	ctx = ensureContext(ctx)

	if err := ctx.Err(); err != nil {
		return domain.AnimeDetails{}, err
	}

	detailsURL := fmt.Sprintf("https://api.myanimelist.net/v2/anime/%d?fields=media_type,start_date,main_picture,related_anime", animeID)
	queue := plan.Queue
	if queue == "" {
		queue = "unknown"
	}

	var lastErr error
	for requestIndex := 0; requestIndex < plan.MaxAttempts; requestIndex++ {
		retryAttempt := requestIndex
		if retryAttempt > 0 {
			client.debug("mal_client", "retrying anime details", "queue", queue, "id", animeID, "attempts", fmt.Sprintf("%d/%d", retryAttempt, plan.MaxAttempts-1))
		} else {
			client.debug("mal_client", "fetching anime details", "queue", queue, "id", animeID)
		}

		requestCtx := ctx
		cancel := func() {}
		if plan.RequestTimeout > 0 {
			requestCtx, cancel = context.WithTimeout(ctx, plan.RequestTimeout)
		}

		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, detailsURL, nil)
		if err != nil {
			cancel()
			return domain.AnimeDetails{}, err
		}
		if err := applyAPIAuth(req, auth); err != nil {
			cancel()
			return domain.AnimeDetails{}, err
		}

		resp, err := client.httpClient.Do(req)
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return domain.AnimeDetails{}, ctx.Err()
			}
			lastErr = err
			if requestIndex == plan.MaxAttempts-1 {
				break
			}
			if err := sleepContext(ctx, time.Duration(retryAttempt+1)*plan.NetworkRetryBase); err != nil {
				return domain.AnimeDetails{}, err
			}
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		if resp.StatusCode == http.StatusOK {
			var details animeDetailsResponse
			if err := json.Unmarshal(body, &details); err != nil {
				return domain.AnimeDetails{}, err
			}
			if details.MediaType == "" {
				return domain.AnimeDetails{}, fmt.Errorf("anime details response missing media_type for id=%d", animeID)
			}

			ids := make([]int, 0, len(details.RelatedAnime))
			related := make([]domain.AnimeRelation, 0, len(details.RelatedAnime))
			for _, rel := range details.RelatedAnime {
				if rel.Node.ID == 0 {
					continue
				}
				ids = append(ids, rel.Node.ID)
				related = append(related, domain.AnimeRelation{
					ID:                    rel.Node.ID,
					Title:                 rel.Node.Title,
					RelationType:          rel.RelationType,
					RelationTypeFormatted: rel.RelationTypeFormatted,
				})
			}

			logArgs := []any{
				"queue", queue,
				"id", animeID,
				"type", details.MediaType,
				"related", len(details.RelatedAnime),
			}
			if queue == "retry" {
				logArgs = append(logArgs, "attempts", fmt.Sprintf("%d/%d", retryAttempt, plan.MaxAttempts-1))
			}
			client.debug("mal_client", "anime details fetched", logArgs...)
			return domain.AnimeDetails{
				ID:             details.ID,
				Title:          details.Title,
				MediaType:      details.MediaType,
				StartDate:      details.StartDate,
				ImageMediumURL: details.MainPicture.Medium,
				ImageLargeURL:  details.MainPicture.Large,
				Related:        related,
				RelatedIDs:     ids,
			}, nil
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("anime details endpoint %d for id=%d: %s", resp.StatusCode, animeID, string(body))
			if requestIndex == plan.MaxAttempts-1 {
				break
			}
			if err := sleepContext(ctx, time.Duration(retryAttempt+1)*plan.StatusRetryBase); err != nil {
				return domain.AnimeDetails{}, err
			}
			continue
		}

		return domain.AnimeDetails{}, fmt.Errorf("anime details endpoint %d for id=%d: %s", resp.StatusCode, animeID, string(body))
	}

	if lastErr == nil {
		lastErr = errors.New("request attempts exhausted without a response")
	}
	return domain.AnimeDetails{}, fmt.Errorf("%w: id=%d: %v", errTransientAnimeDetails, animeID, lastErr)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	ctx = ensureContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (client *MyAnimeListClient) debug(component, msg string, args ...any) {
	if client != nil && client.logger != nil {
		client.logger.Debug(component, msg, args...)
	}
}

func (client *MyAnimeListClient) warn(component, msg string, args ...any) {
	if client != nil && client.logger != nil {
		client.logger.Warn(component, msg, args...)
	}
}
