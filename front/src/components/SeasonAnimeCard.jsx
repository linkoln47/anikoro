import { formatAirStart, formatMediaType } from '../entities/anime/animeFormatters'

function SeasonAnimeCard({ anime, onSelect = () => {} }) {
  const imageUrl = anime.image_large_url || anime.image_medium_url || ''
  const episodes = anime.num_episodes > 0 ? `${anime.num_episodes} ep` : 'TBA'

  return (
    <button
      type="button"
      className="season-card"
      onClick={() => onSelect(anime.id)}
      title={anime.title}
      aria-label={`Open franchise view for ${anime.title}`}
    >
      <div className="season-card-cover">
        {imageUrl ? (
          <img className="season-card-image" src={imageUrl} alt="" loading="lazy" />
        ) : (
          <div className="season-card-cover-fallback" aria-hidden="true" />
        )}
        <span className={`season-card-type type-${anime.media_type}`}>
          {formatMediaType(anime.media_type)}
        </span>
      </div>

      <div className="season-card-body">
        <h3 className="season-card-title">{anime.title}</h3>
        <div className="season-card-meta">
          <span>{formatAirStart(anime.start_date)}</span>
          <span>{episodes}</span>
        </div>
      </div>
    </button>
  )
}

export default SeasonAnimeCard
