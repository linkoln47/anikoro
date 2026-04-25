package main

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
			Score              int `json:"score"`
			NumEpisodesWatched int `json:"num_episodes_watched"`
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

func applyMALAuth(req *http.Request, auth MALAuth) error {
	if auth.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+auth.BearerToken)
		return nil
	}
	if auth.ClientID != "" {
		req.Header.Set("X-MAL-CLIENT-ID", auth.ClientID)
		return nil
	}
	return errors.New("MAL API authorization is required")
}

func (a *App) FetchCompletedList(ctx context.Context, token string) ([]CompletedAnimeEntry, error) {
	return a.fetchCompletedAnimeEntriesWithAuthContext(ctx, malAnimeListURL, bearerMALAuth(token))
}

func (a *App) FetchPublicCompletedList(ctx context.Context, username string) ([]CompletedAnimeEntry, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, errors.New("MAL username is required")
	}

	listURL := fmt.Sprintf(malPublicAnimeListURLFormat, url.PathEscape(username))
	return a.fetchCompletedAnimeEntriesWithAuthContext(ctx, listURL, clientIDMALAuth(a.Config.ClientID))
}

func (a *App) FetchAnimeDetails(
	ctx context.Context,
	auth MALAuth,
	animeID int,
	cache AnimeDetailsCacheStore,
	mode AnimeDetailsFetchMode,
) (AnimeDetails, error) {
	if mode == animeDetailsFetchRetry {
		return a.fetchAnimeDetailsRetryWithAuthContext(ctx, auth, animeID)
	}
	return a.fetchAnimeDetailsPrimaryWithAuthContext(ctx, auth, animeID, cache)
}

func (a *App) FetchCompletedAnimeEntries(token string) ([]CompletedAnimeEntry, error) {
	return a.FetchCompletedAnimeEntriesWithContext(context.Background(), token)
}

func (a *App) FetchCompletedAnimeEntriesWithContext(ctx context.Context, token string) ([]CompletedAnimeEntry, error) {
	return a.FetchCompletedList(ctx, token)
}

func (a *App) FetchPublicCompletedAnimeEntriesWithContext(ctx context.Context, username string) ([]CompletedAnimeEntry, error) {
	return a.FetchPublicCompletedList(ctx, username)
}

func (a *App) fetchCompletedAnimeEntriesWithAuthContext(ctx context.Context, listURL string, auth MALAuth) ([]CompletedAnimeEntry, error) {
	ctx = ensureContext(ctx)

	u, err := url.Parse(listURL)
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("status", "completed")
	q.Set("limit", "100")
	q.Set("fields", "list_status")
	u.RawQuery = q.Encode()

	var allEntries []CompletedAnimeEntry
	nextURL := u.String()
	page := 1
	for nextURL != "" {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		a.logDebug("mal_client", "requesting animelist page", "page", page, "url", nextURL)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, err
		}
		if err := applyMALAuth(req, auth); err != nil {
			return nil, err
		}

		resp, err := a.HTTPClient.Do(req)
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
			allEntries = append(allEntries, CompletedAnimeEntry{
				ID:                 item.Node.ID,
				Title:              item.Node.Title,
				Score:              item.ListStatus.Score,
				NumEpisodesWatched: item.ListStatus.NumEpisodesWatched,
			})
		}

		a.logDebug("mal_client", "received animelist page", "page", page, "entries", len(parsed.Data))
		nextURL = parsed.Paging.Next
		page++
	}

	return allEntries, nil
}

func (a *App) fetchAnimeDetailsPrimary(token string, animeID int, cache AnimeDetailsCacheStore) (AnimeDetails, error) {
	return a.fetchAnimeDetailsPrimaryWithContext(context.Background(), token, animeID, cache)
}

func (a *App) fetchAnimeDetailsPrimaryWithContext(ctx context.Context, token string, animeID int, cache AnimeDetailsCacheStore) (AnimeDetails, error) {
	return a.fetchAnimeDetailsPrimaryWithAuthContext(ctx, bearerMALAuth(token), animeID, cache)
}

func (a *App) fetchAnimeDetailsPrimaryWithAuthContext(ctx context.Context, auth MALAuth, animeID int, cache AnimeDetailsCacheStore) (AnimeDetails, error) {
	ctx = ensureContext(ctx)

	if err := ctx.Err(); err != nil {
		return AnimeDetails{}, err
	}
	if animeID == 0 {
		return AnimeDetails{}, nil
	}

	cached, ok := cache.Lookup(animeID)
	switch {
	case ok && cached.IsFresh(time.Now()):
		a.logDebug("mal_client", "anime details cache hit", "id", animeID)
		details := domain.CloneAnimeDetails(cached.Details)
		details.ID = animeID
		return details, nil
	case ok && cached.IsUsable():
		a.logDebug("mal_client", "anime details cache stale, refreshing", "id", animeID)
	case ok:
		a.logDebug("mal_client", "anime details cache unusable, refreshing", "id", animeID)
	default:
		a.logDebug("mal_client", "anime details cache miss", "id", animeID)
	}

	details, err := a.requestAnimeDetailsWithPlanAndAuthContext(ctx, auth, animeID, animeDetailsRequestPlan{
		MaxAttempts:      1,
		NetworkRetryBase: animeDetailsNetworkRetryBase,
		Queue:            "primary",
		RequestTimeout:   animeDetailsPrimaryTimeout,
		StatusRetryBase:  animeDetailsStatusRetryBase,
	})
	if err != nil {
		if errors.Is(err, errTransientAnimeDetails) && ok && cached.IsUsable() {
			a.logWarn("mal_client", "using stale cache after transient MAL error", "id", animeID, "err", err)
			details := domain.CloneAnimeDetails(cached.Details)
			details.ID = animeID
			return details, nil
		}
		return AnimeDetails{}, err
	}

	if err := cache.StoreResolved(animeID, details); err != nil {
		a.logWarn("cache", "cannot flush details cache batch", "id", animeID, "err", err)
	}
	a.logDebug("mal_client", "anime details cache updated", "id", animeID)
	return details, nil
}

func (a *App) fetchAnimeDetailsRetry(token string, animeID int) (AnimeDetails, error) {
	return a.fetchAnimeDetailsRetryWithContext(context.Background(), token, animeID)
}

func (a *App) fetchAnimeDetailsRetryWithContext(ctx context.Context, token string, animeID int) (AnimeDetails, error) {
	return a.fetchAnimeDetailsRetryWithAuthContext(ctx, bearerMALAuth(token), animeID)
}

func (a *App) fetchAnimeDetailsRetryWithAuthContext(ctx context.Context, auth MALAuth, animeID int) (AnimeDetails, error) {
	return a.requestAnimeDetailsWithPlanAndAuthContext(ctx, auth, animeID, animeDetailsRequestPlan{
		MaxAttempts:      animeDetailsMaxAttempts,
		NetworkRetryBase: animeDetailsNetworkRetryBase,
		Queue:            "retry",
		RequestTimeout:   animeDetailsRetryTimeout,
		StatusRetryBase:  animeDetailsStatusRetryBase,
	})
}

func (a *App) requestAnimeDetailsWithPlan(token string, animeID int, plan animeDetailsRequestPlan) (AnimeDetails, error) {
	return a.requestAnimeDetailsWithPlanAndContext(context.Background(), token, animeID, plan)
}

func (a *App) requestAnimeDetailsWithPlanAndContext(ctx context.Context, token string, animeID int, plan animeDetailsRequestPlan) (AnimeDetails, error) {
	return a.requestAnimeDetailsWithPlanAndAuthContext(ctx, bearerMALAuth(token), animeID, plan)
}

func (a *App) requestAnimeDetailsWithPlanAndAuthContext(ctx context.Context, auth MALAuth, animeID int, plan animeDetailsRequestPlan) (AnimeDetails, error) {
	ctx = ensureContext(ctx)

	if err := ctx.Err(); err != nil {
		return AnimeDetails{}, err
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
			a.logDebug("mal_client", "retrying anime details", "queue", queue, "id", animeID, "attempts", fmt.Sprintf("%d/%d", retryAttempt, plan.MaxAttempts-1))
		} else {
			a.logDebug("mal_client", "fetching anime details", "queue", queue, "id", animeID)
		}

		requestCtx := ctx
		cancel := func() {}
		if plan.RequestTimeout > 0 {
			requestCtx, cancel = context.WithTimeout(ctx, plan.RequestTimeout)
		}

		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, detailsURL, nil)
		if err != nil {
			cancel()
			return AnimeDetails{}, err
		}
		if err := applyMALAuth(req, auth); err != nil {
			cancel()
			return AnimeDetails{}, err
		}

		resp, err := a.HTTPClient.Do(req)
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return AnimeDetails{}, ctx.Err()
			}
			lastErr = err
			if requestIndex == plan.MaxAttempts-1 {
				break
			}
			if err := sleepContext(ctx, time.Duration(retryAttempt+1)*plan.NetworkRetryBase); err != nil {
				return AnimeDetails{}, err
			}
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		if resp.StatusCode == http.StatusOK {
			var details animeDetailsResponse
			if err := json.Unmarshal(body, &details); err != nil {
				return AnimeDetails{}, err
			}
			if details.MediaType == "" {
				return AnimeDetails{}, fmt.Errorf("anime details response missing media_type for id=%d", animeID)
			}

			ids := make([]int, 0, len(details.RelatedAnime))
			related := make([]AnimeRelation, 0, len(details.RelatedAnime))
			for _, rel := range details.RelatedAnime {
				if rel.Node.ID == 0 {
					continue
				}
				ids = append(ids, rel.Node.ID)
				related = append(related, AnimeRelation{
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
			a.logDebug("mal_client", "anime details fetched", logArgs...)
			return AnimeDetails{
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
				return AnimeDetails{}, err
			}
			continue
		}

		return AnimeDetails{}, fmt.Errorf("anime details endpoint %d for id=%d: %s", resp.StatusCode, animeID, string(body))
	}

	if lastErr == nil {
		lastErr = errors.New("request attempts exhausted without a response")
	}
	return AnimeDetails{}, fmt.Errorf("%w: id=%d: %v", errTransientAnimeDetails, animeID, lastErr)
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
