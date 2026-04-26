package app

import (
	"test/internal/ports"
	"test/internal/usecase"
)

type AnimeReadRepository = ports.AnimeReadRepository
type AnimeQueryService = usecase.AnimeQueryService

func newAnimeQueryService(repo AnimeReadRepository) *AnimeQueryService {
	return usecase.NewAnimeQueryService(repo)
}

func (a *App) animeQueryService() *AnimeQueryService {
	return a.AnimeQueries
}
