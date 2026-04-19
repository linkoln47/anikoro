package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const malAnimeListURL = "https://api.myanimelist.net/v2/users/@me/animelist"

var errTransientAnimeDetails = errors.New("transient anime details error")

var (
	animeDetailsMaxAttempts      = 4 // enter >0, otherwise requestAnimeDetailsWithPlan will break
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
	ID           int    `json:"id"`
	Title        string `json:"title"`
	MediaType    string `json:"media_type"`
	RelatedAnime []struct {
		Node struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
		} `json:"node"`
	} `json:"related_anime"`
}

type AnimeDetailsInfo struct {
	RelatedIDs []int
	MediaType  string
}

func (a *App) FetchCompletedAnimeEntries(token string) ([]AnimeEntry, error) {
	return a.FetchCompletedAnimeEntriesWithContext(context.Background(), token)
}

func (a *App) FetchCompletedAnimeEntriesWithContext(ctx context.Context, token string) ([]AnimeEntry, error) {
	ctx = ensureContext(ctx)

	u, err := url.Parse(malAnimeListURL)
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
		req.Header.Set("Authorization", "Bearer "+token)

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

type AnimeDetailsRequestPlan struct {
	MaxAttempts      int
	NetworkRetryBase time.Duration
	Queue            string
	RequestTimeout   time.Duration
	StatusRetryBase  time.Duration
}

func (a *App) FetchAnimeDetailsPrimary(token string, animeID int, cache *AnimeDetailsCacheStore) (AnimeDetailsInfo, error) {
	return a.FetchAnimeDetailsPrimaryWithContext(context.Background(), token, animeID, cache)
}

func (a *App) FetchAnimeDetailsPrimaryWithContext(ctx context.Context, token string, animeID int, cache *AnimeDetailsCacheStore) (AnimeDetailsInfo, error) {
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
		return cached.toInfo(), nil
	case ok && cached.isUsable():
		a.logDebug("mal_client", "anime details cache stale, refreshing", "id", animeID)
	case ok:
		a.logDebug("mal_client", "anime details cache unusable, refreshing", "id", animeID)
	default:
		a.logDebug("mal_client", "anime details cache miss", "id", animeID)
	}

	details, err := a.RequestAnimeDetailsWithPlanAndContext(ctx, token, animeID, AnimeDetailsRequestPlan{
		MaxAttempts:      1,
		NetworkRetryBase: animeDetailsNetworkRetryBase,
		Queue:            "primary",
		RequestTimeout:   animeDetailsPrimaryTimeout,
		StatusRetryBase:  animeDetailsStatusRetryBase,
	})
	if err != nil {
		if errors.Is(err, errTransientAnimeDetails) && ok && cached.isUsable() {
			a.logWarn("mal_client", "using stale cache after transient MAL error", "id", animeID, "err", err)
			return cached.toInfo(), nil
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
	return a.RequestAnimeDetailsWithPlanAndContext(ctx, token, animeID, AnimeDetailsRequestPlan{
		MaxAttempts:      animeDetailsMaxAttempts,
		NetworkRetryBase: animeDetailsNetworkRetryBase,
		Queue:            "retry",
		RequestTimeout:   animeDetailsRetryTimeout,
		StatusRetryBase:  animeDetailsStatusRetryBase,
	})
}

func (a *App) RequestAnimeDetailsWithPlan(token string, animeID int, plan AnimeDetailsRequestPlan) (AnimeDetailsInfo, error) {
	return a.RequestAnimeDetailsWithPlanAndContext(context.Background(), token, animeID, plan)
}

func (a *App) RequestAnimeDetailsWithPlanAndContext(ctx context.Context, token string, animeID int, plan AnimeDetailsRequestPlan) (AnimeDetailsInfo, error) {
	ctx = ensureContext(ctx)

	if err := ctx.Err(); err != nil {
		return AnimeDetailsInfo{}, err
	}

	detailsURL := fmt.Sprintf("https://api.myanimelist.net/v2/anime/%d?fields=related_anime,media_type", animeID)
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
		req.Header.Set("Authorization", "Bearer "+token)

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
			for _, rel := range details.RelatedAnime {
				if rel.Node.ID != 0 {
					ids = append(ids, rel.Node.ID)
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
			return AnimeDetailsInfo{RelatedIDs: ids, MediaType: details.MediaType}, nil
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
