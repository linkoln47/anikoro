function StatsGrid({ stats, isLoading }) {
  const cards = [
    { label: 'Series', value: stats.series_count },
    { label: 'Movies', value: stats.movies_count },
    { label: 'Total', value: stats.total_count },
  ]

  return (
    <section className="stats-grid">
      {cards.map((card) => (
        <article key={card.label} className="panel stat-card">
          <span className="stat-label">{card.label}</span>
          {isLoading ? (
            <div className="skeleton-line skeleton-stat-value" aria-hidden="true" />
          ) : (
            <strong>{card.value}</strong>
          )}
        </article>
      ))}
    </section>
  )
}

export default StatsGrid
