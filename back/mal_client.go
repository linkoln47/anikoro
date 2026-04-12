package main

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
	animeDetailsPrimaryTimeout   = 5 * time.Second
	animeDetailsRetryTimeout     = 20 * time.Second
)

type animeEntry struct {
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

type animeDetailsInfo struct {
	RelatedIDs []int
	MediaType  string
}

func fetchCompletedAnimeEntries(token string) ([]animeEntry, error) {
	u, err := url.Parse(malAnimeListURL)
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("status", "completed")
	q.Set("limit", "100")
	q.Set("fields", "list_status")
	u.RawQuery = q.Encode()

	var allEntries []animeEntry
	nextURL := u.String()
	page := 1
	for nextURL != "" {
		logDebug("mal_client", "requesting animelist page", "page", page, "url", nextURL)
		req, err := http.NewRequest(http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
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
			allEntries = append(allEntries, animeEntry{
				ID:                 item.Node.ID,
				Title:              item.Node.Title,
				Score:              item.ListStatus.Score,
				NumEpisodesWatched: item.ListStatus.NumEpisodesWatched,
			})
		}

		logDebug("mal_client", "received animelist page", "page", page, "entries", len(parsed.Data))
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

func fetchAnimeDetailsPrimary(token string, animeID int, cache *animeDetailsCacheStore) (animeDetailsInfo, error) {
	if animeID == 0 {
		return animeDetailsInfo{}, nil
	}

	cached, ok := cache.Lookup(animeID)
	switch {
	case ok && cached.isFresh(time.Now()):
		logDebug("mal_client", "anime details cache hit", "id", animeID)
		return cached.toInfo(), nil
	case ok && cached.isUsable():
		logDebug("mal_client", "anime details cache stale, refreshing", "id", animeID)
	case ok:
		logDebug("mal_client", "anime details cache unusable, refreshing", "id", animeID)
	default:
		logDebug("mal_client", "anime details cache miss", "id", animeID)
	}

	details, err := requestAnimeDetailsWithPlan(token, animeID, animeDetailsRequestPlan{
		MaxAttempts:      1,
		NetworkRetryBase: animeDetailsNetworkRetryBase,
		Queue:            "primary",
		RequestTimeout:   animeDetailsPrimaryTimeout,
		StatusRetryBase:  animeDetailsStatusRetryBase,
	})
	if err != nil {
		if errors.Is(err, errTransientAnimeDetails) && ok && cached.isUsable() {
			logWarn("mal_client", "using stale cache after transient MAL error", "id", animeID, "err", err)
			return cached.toInfo(), nil
		}
		return animeDetailsInfo{}, err
	}

	if err := cache.StoreResolved(animeID, details); err != nil {
		logWarn("cache", "cannot flush details cache batch", "id", animeID, "err", err)
	}
	logDebug("mal_client", "anime details cache updated", "id", animeID)
	return details, nil
}

func fetchAnimeDetailsRetry(token string, animeID int) (animeDetailsInfo, error) {
	return requestAnimeDetailsWithPlan(token, animeID, animeDetailsRequestPlan{
		MaxAttempts:      animeDetailsMaxAttempts,
		NetworkRetryBase: animeDetailsNetworkRetryBase,
		Queue:            "retry",
		RequestTimeout:   animeDetailsRetryTimeout,
		StatusRetryBase:  animeDetailsStatusRetryBase,
	})
}

func requestAnimeDetailsWithPlan(token string, animeID int, plan animeDetailsRequestPlan) (animeDetailsInfo, error) {
	detailsURL := fmt.Sprintf("https://api.myanimelist.net/v2/anime/%d?fields=related_anime,media_type", animeID)
	queue := plan.Queue
	if queue == "" {
		queue = "unknown"
	}

	var lastErr error
	for requestIndex := 0; requestIndex < plan.MaxAttempts; requestIndex++ {
		retryAttempt := requestIndex
		if retryAttempt > 0 {
			logDebug("mal_client", "retrying anime details", "queue", queue, "id", animeID, "attempts", fmt.Sprintf("%d/%d", retryAttempt, plan.MaxAttempts-1))
		} else {
			logDebug("mal_client", "fetching anime details", "queue", queue, "id", animeID)
		}

		ctx := context.Background()
		cancel := func() {}
		if plan.RequestTimeout > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), plan.RequestTimeout)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, detailsURL, nil)
		if err != nil {
			cancel()
			return animeDetailsInfo{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			if requestIndex == plan.MaxAttempts-1 {
				break
			}
			if plan.NetworkRetryBase > 0 {
				time.Sleep(time.Duration(retryAttempt+1) * plan.NetworkRetryBase)
			}
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		if resp.StatusCode == http.StatusOK {
			var details animeDetailsResponse
			if err := json.Unmarshal(body, &details); err != nil {
				return animeDetailsInfo{}, err
			}
			if details.MediaType == "" {
				return animeDetailsInfo{}, fmt.Errorf("anime details response missing media_type for id=%d", animeID)
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
			logDebug("mal_client", "anime details fetched", logArgs...)
			return animeDetailsInfo{RelatedIDs: ids, MediaType: details.MediaType}, nil
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("anime details endpoint %d for id=%d: %s", resp.StatusCode, animeID, string(body))
			if requestIndex == plan.MaxAttempts-1 {
				break
			}
			if plan.StatusRetryBase > 0 {
				time.Sleep(time.Duration(retryAttempt+1) * plan.StatusRetryBase)
			}
			continue
		}

		return animeDetailsInfo{}, fmt.Errorf("anime details endpoint %d for id=%d: %s", resp.StatusCode, animeID, string(body))
	}

	if lastErr == nil {
		lastErr = errors.New("request attempts exhausted without a response")
	}
	return animeDetailsInfo{}, fmt.Errorf("%w: id=%d: %v", errTransientAnimeDetails, animeID, lastErr)
}
