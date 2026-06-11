package mal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"test/internal/domain"
	"test/internal/ports"
)

const malUpdateListStatusURLFormat = "https://api.myanimelist.net/v2/anime/%d/my_list_status"

var _ ports.MALAnimeListWriter = (*MyAnimeListClient)(nil)

// myListStatusResponse mirrors the MAL my_list_status payload. Note the MAL
// quirk: the request parameter is num_watched_episodes while the response
// field is num_episodes_watched.
type myListStatusResponse struct {
	Status             string `json:"status"`
	Score              int    `json:"score"`
	NumEpisodesWatched int    `json:"num_episodes_watched"`
}

func (client *MyAnimeListClient) UpdateAnimeListStatus(ctx context.Context, token string, animeID int, patch domain.UserAnimeListPatch) (domain.AnimeUserListState, error) {
	ctx = ensureContext(ctx)

	if animeID <= 0 {
		return domain.AnimeUserListState{}, fmt.Errorf("anime id must be positive")
	}
	if patch.IsEmpty() {
		return domain.AnimeUserListState{}, domain.ErrEmptyAnimeListPatch
	}

	form := url.Values{}
	if patch.Status != nil {
		form.Set("status", string(*patch.Status))
	}
	if patch.Score != nil {
		form.Set("score", strconv.Itoa(*patch.Score))
	}
	if patch.WatchedEpisodes != nil {
		form.Set("num_watched_episodes", strconv.Itoa(*patch.WatchedEpisodes))
	}

	updateURL := fmt.Sprintf(malUpdateListStatusURLFormat, animeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, updateURL, strings.NewReader(form.Encode()))
	if err != nil {
		return domain.AnimeUserListState{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := applyAPIAuth(req, bearerAuth(token)); err != nil {
		return domain.AnimeUserListState{}, err
	}

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return domain.AnimeUserListState{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return domain.AnimeUserListState{}, fmt.Errorf("my_list_status endpoint %d for id=%d: %s", resp.StatusCode, animeID, string(body))
	}

	var parsed myListStatusResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return domain.AnimeUserListState{}, err
	}

	status, ok := domain.NormalizeAnimeListStatus(parsed.Status)
	if !ok {
		return domain.AnimeUserListState{}, fmt.Errorf("MAL returned unsupported list status %q for anime %d", parsed.Status, animeID)
	}

	client.debug("mal_client", "anime list status updated", "id", animeID, "status", string(status), "score", parsed.Score, "watched", parsed.NumEpisodesWatched)
	return domain.AnimeUserListState{
		Score:           parsed.Score,
		WatchedEpisodes: parsed.NumEpisodesWatched,
		ListStatus:      string(status),
	}, nil
}

func (client *MyAnimeListClient) DeleteAnimeListStatus(ctx context.Context, token string, animeID int) error {
	ctx = ensureContext(ctx)

	if animeID <= 0 {
		return fmt.Errorf("anime id must be positive")
	}

	deleteURL := fmt.Sprintf(malUpdateListStatusURLFormat, animeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
	if err != nil {
		return err
	}
	if err := applyAPIAuth(req, bearerAuth(token)); err != nil {
		return err
	}

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	// MAL answers 404 when the entry is already absent; treat the delete as
	// idempotent in that case.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("my_list_status delete endpoint %d for id=%d: %s", resp.StatusCode, animeID, string(body))
	}

	client.debug("mal_client", "anime list status deleted", "id", animeID, "status_code", resp.StatusCode)
	return nil
}
