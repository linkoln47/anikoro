function Footer() {
  return (
    <footer className="site-footer">
      <p className="site-footer-privacy">
        We never store your MyAnimeList password &mdash; sign-in uses MAL&apos;s
        official OAuth. We only keep your anime data, stored encrypted.
      </p>
      <p className="site-footer-meta">
        anikoro is an unofficial, non-commercial project and is not affiliated
        with or endorsed by MyAnimeList. Anime data provided by{' '}
        <a
          className="site-footer-link"
          href="https://myanimelist.net"
          target="_blank"
          rel="noopener noreferrer"
        >
          MyAnimeList
        </a>
        .
      </p>
      <p className="site-footer-copyright">&copy; 2026 Fedunov Aleksei</p>
    </footer>
  )
}

export default Footer
