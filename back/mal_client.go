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
)

const (
	malAnimeListURL             = "https://api.myanimelist.net/v2/users/@me/animelist"
	malPublicAnimeListURLFormat = "https://api.myanimelist.net/v2/users/%s/animelist"
)

var errTransientAnimeDetails = errors.New("transient anime details error")

var (
	animeDetailsMaxAttempts      = 4 // enter >0, otherwise requestAnimeDetailsWithPlanAndContext will break
	animeDetailsNetworkRetryBase = 500 * time.Millisecond
	animeDetailsStatusRetryBase  = 700 * time.Millisecond
	animeDetailsPrimaryTimeout   = 3 * time.Second
	animeDetailsRetryTimeout     = 25 * time.Second
)

type AnimeEntry struct {
	ID                 int
	Title              string
	Score              int
	NumEpisodesWatched int
}

type malAPIAuth struct {
	bearerToken string
	clientID    string
}

func bearerMALAuth(token string) malAPIAuth {
	return malAPIAuth{bearerToken: strings.TrimSpace(token)}
}

func clientIDMALAuth(clientID string) malAPIAuth {
	return malAPIAuth{clientID: strings.TrimSpace(clientID)}
}

func (auth malAPIAuth) apply(req *http.Request) error {
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

type AnimeRelationInfo struct {
	ID                    int    `json:"id"`
	Title                 string `json:"title"`
	RelationType          string `json:"relation_type"`
	RelationTypeFormatted string `json:"relation_type_formatted"`
}

type AnimeDetailsInfo struct {
	ID             int
	Title          string
	MediaType      string
	StartDate      string
	ImageMediumURL string
	ImageLargeURL  string
	Related        []AnimeRelationInfo
	RelatedIDs     []int
}

func (a *App) FetchCompletedAnimeEntries(token string) ([]AnimeEntry, error) {
	return a.FetchCompletedAnimeEntriesWithContext(context.Background(), token)
}

func (a *App) FetchCompletedAnimeEntriesWithContext(ctx context.Context, token string) ([]AnimeEntry, error) {
	return a.fetchCompletedAnimeEntriesWithAuthContext(ctx, malAnimeListURL, bearerMALAuth(token))
}

func (a *App) FetchPublicCompletedAnimeEntriesWithContext(ctx context.Context, username string) ([]AnimeEntry, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, errors.New("MAL username is required")
	}

	listURL := fmt.Sprintf(malPublicAnimeListURLFormat, url.PathEscape(username))
	return a.fetchCompletedAnimeEntriesWithAuthContext(ctx, listURL, clientIDMALAuth(a.Config.ClientID))
}

func (a *App) fetchCompletedAnimeEntriesWithAuthContext(ctx context.Context, listURL string, auth malAPIAuth) ([]AnimeEntry, error) {
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

	var allEntries []AnimeEntry
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
		if err := auth.apply(req); err != nil {
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
			allEntries = append(allEntries, AnimeEntry{
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

type animeDetailsRequestPlan struct {
	MaxAttempts      int
	NetworkRetryBase time.Duration
	Queue            string
	RequestTimeout   time.Duration
	StatusRetryBase  time.Duration
}

func (a *App) fetchAnimeDetailsPrimary(token string, animeID int, cache *animeDetailsCacheStore) (AnimeDetailsInfo, error) {
	return a.fetchAnimeDetailsPrimaryWithContext(context.Background(), token, animeID, cache)
}

func (a *App) fetchAnimeDetailsPrimaryWithContext(ctx context.Context, token string, animeID int, cache *animeDetailsCacheStore) (AnimeDetailsInfo, error) {
	return a.fetchAnimeDetailsPrimaryWithAuthContext(ctx, bearerMALAuth(token), animeID, cache)
}

func (a *App) fetchAnimeDetailsPrimaryWithAuthContext(ctx context.Context, auth malAPIAuth, animeID int, cache *animeDetailsCacheStore) (AnimeDetailsInfo, error) {
	ctx = ensureContext(ctx)

	if err := ctx.Err(); err != nil {
		return AnimeDetailsInfo{}, err
	}
	if animeID == 0 {
		return AnimeDetailsInfo{}, nil
	}

	cached, ok := cache.Lookup(animeID)
	switch {
	case ok && cached.isFresh(time.Now()):
		a.logDebug("mal_client", "anime details cache hit", "id", animeID)
		details := cached.toInfo()
		details.ID = animeID
		return details, nil
	case ok && cached.isUsable():
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
		if errors.Is(err, errTransientAnimeDetails) && ok && cached.isUsable() {
			a.logWarn("mal_client", "using stale cache after transient MAL error", "id", animeID, "err", err)
			details := cached.toInfo()
			details.ID = animeID
			return details, nil
		}
		return AnimeDetailsInfo{}, err
	}

	if err := cache.StoreResolved(animeID, details); err != nil {
		a.logWarn("cache", "cannot flush details cache batch", "id", animeID, "err", err)
	}
	a.logDebug("mal_client", "anime details cache updated", "id", animeID)
	return details, nil
}

func (a *App) fetchAnimeDetailsRetry(token string, animeID int) (AnimeDetailsInfo, error) {
	return a.fetchAnimeDetailsRetryWithContext(context.Background(), token, animeID)
}

func (a *App) fetchAnimeDetailsRetryWithContext(ctx context.Context, token string, animeID int) (AnimeDetailsInfo, error) {
	return a.fetchAnimeDetailsRetryWithAuthContext(ctx, bearerMALAuth(token), animeID)
}

func (a *App) fetchAnimeDetailsRetryWithAuthContext(ctx context.Context, auth malAPIAuth, animeID int) (AnimeDetailsInfo, error) {
	return a.requestAnimeDetailsWithPlanAndAuthContext(ctx, auth, animeID, animeDetailsRequestPlan{
		MaxAttempts:      animeDetailsMaxAttempts,
		NetworkRetryBase: animeDetailsNetworkRetryBase,
		Queue:            "retry",
		RequestTimeout:   animeDetailsRetryTimeout,
		StatusRetryBase:  animeDetailsStatusRetryBase,
	})
}

func (a *App) requestAnimeDetailsWithPlan(token string, animeID int, plan animeDetailsRequestPlan) (AnimeDetailsInfo, error) {
	return a.requestAnimeDetailsWithPlanAndContext(context.Background(), token, animeID, plan)
}

func (a *App) requestAnimeDetailsWithPlanAndContext(ctx context.Context, token string, animeID int, plan animeDetailsRequestPlan) (AnimeDetailsInfo, error) {
	return a.requestAnimeDetailsWithPlanAndAuthContext(ctx, bearerMALAuth(token), animeID, plan)
}

func (a *App) requestAnimeDetailsWithPlanAndAuthContext(ctx context.Context, auth malAPIAuth, animeID int, plan animeDetailsRequestPlan) (AnimeDetailsInfo, error) {
	ctx = ensureContext(ctx)

	if err := ctx.Err(); err != nil {
		return AnimeDetailsInfo{}, err
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
			return AnimeDetailsInfo{}, err
		}
		if err := auth.apply(req); err != nil {
			cancel()
			return AnimeDetailsInfo{}, err
		}

		resp, err := a.HTTPClient.Do(req)
		if err != nil {
			cancel()
			if ctx.Err() != nil {
				return AnimeDetailsInfo{}, ctx.Err()
			}
			lastErr = err
			if requestIndex == plan.MaxAttempts-1 {
				break
			}
			if err := sleepContext(ctx, time.Duration(retryAttempt+1)*plan.NetworkRetryBase); err != nil {
				return AnimeDetailsInfo{}, err
			}
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		if resp.StatusCode == http.StatusOK {
			var details animeDetailsResponse
			if err := json.Unmarshal(body, &details); err != nil {
				return AnimeDetailsInfo{}, err
			}
			if details.MediaType == "" {
				return AnimeDetailsInfo{}, fmt.Errorf("anime details response missing media_type for id=%d", animeID)
			}

			ids := make([]int, 0, len(details.RelatedAnime))
			related := make([]AnimeRelationInfo, 0, len(details.RelatedAnime))
			for _, rel := range details.RelatedAnime {
				if rel.Node.ID != 0 {
					ids = append(ids, rel.Node.ID)
					related = append(related, AnimeRelationInfo{
						ID:                    rel.Node.ID,
						Title:                 rel.Node.Title,
						RelationType:          rel.RelationType,
						RelationTypeFormatted: rel.RelationTypeFormatted,
					})
				}
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
			return AnimeDetailsInfo{
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
				return AnimeDetailsInfo{}, err
			}
			continue
		}

		return AnimeDetailsInfo{}, fmt.Errorf("anime details endpoint %d for id=%d: %s", resp.StatusCode, animeID, string(body))
	}

	if lastErr == nil {
		lastErr = errors.New("request attempts exhausted without a response")
	}
	return AnimeDetailsInfo{}, fmt.Errorf("%w: id=%d: %v", errTransientAnimeDetails, animeID, lastErr)
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
