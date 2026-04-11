package main

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

const (
	detailsCacheName = ".mal_anime_details_cache.json"
	detailsCacheTTL  = 168 * time.Hour
)

var detailsCachePath = appFilePath(detailsCacheName)

type animeDetailsCacheItem struct {
	RelatedIDs []int     `json:"related_ids"`
	MediaType  string    `json:"media_type"`
	UpdatedAt  time.Time `json:"updated_at"`
	Resolved   bool      `json:"resolved,omitempty"`
}

func (item animeDetailsCacheItem) isUsable() bool {
	return item.Resolved || item.MediaType != ""
}

func (item animeDetailsCacheItem) isFresh(now time.Time) bool {
	return item.isUsable() && !item.UpdatedAt.IsZero() && now.Sub(item.UpdatedAt) <= detailsCacheTTL
}

func (item animeDetailsCacheItem) toInfo() animeDetailsInfo {
	return animeDetailsInfo{
		RelatedIDs: item.RelatedIDs,
		MediaType:  item.MediaType,
	}
}

func loadDetailsCache() (map[int]animeDetailsCacheItem, error) {
	b, err := os.ReadFile(detailsCachePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logInfo("cache", "details cache file not found, a new cache will be created", "path", detailsCachePath)
			return map[int]animeDetailsCacheItem{}, nil
		}
		return nil, err
	}

	logDebug("cache", "details cache file loaded", "path", detailsCachePath)

	var cache map[int]animeDetailsCacheItem
	if err := json.Unmarshal(b, &cache); err != nil {
		return nil, err
	}
	if cache == nil {
		cache = map[int]animeDetailsCacheItem{}
	}
	return cache, nil
}

func saveDetailsCache(cache map[int]animeDetailsCacheItem) error {
	b, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return writeFileWithChangeLog(detailsCachePath, b, 0o644, "Cache file")
}
