package main

import "test/internal/usecase"

type AnimeReadRepository = usecase.AnimeReadRepository
type AnimeQueryService = usecase.AnimeQueryService

func newAnimeQueryService(repo AnimeReadRepository) *AnimeQueryService {
	return usecase.NewAnimeQueryService(repo)
}

func (a *App) animeQueryService() *AnimeQueryService {
	if a.AnimeQueries != nil {
		return a.AnimeQueries
	}
	a.AnimeQueries = newAnimeQueryService(newPostgresAnimeRepository(a.DB))
	return a.AnimeQueries
}
