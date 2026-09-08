/**
 * The "home session" is the session a browser tab was opened FOR — by
 * `handler ui-open <session>`, or by landing cold on a session detail page.
 *
 * The URL is the carrier: the `home` parameter is attached to every URL the
 * tab ever has and is never stripped. A cmux pane that sleeps and is restored
 * comes back with a URL and a brand new JS context, so anything held only in
 * browser storage would be gone. sessionStorage is kept as a second copy, not
 * the source of truth.
 *
 * The trade-off, taken deliberately: a URL copied out of the address bar
 * carries the home and will home whoever pastes it. For a localhost tool that
 * is a fair price for surviving restore.
 */

export const HOME_PARAM = "home"

const STORAGE_KEY = "handler.homeSession"

/**
 * sessionStorage throws outright in some contexts (private windows with site
 * data blocked). A navigation aid is never worth taking the page down for.
 */
function readStored(): string | null {
  try {
    return window.sessionStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

function writeStored(sessionId: string): void {
  try {
    window.sessionStorage.setItem(STORAGE_KEY, sessionId)
  } catch {
    // ignore
  }
}

/** The session id if `path` is a session detail page, else null. */
export function sessionIdFromPath(pathname: string): string | null {
  const match = pathname.match(/^\/sessions\/(.+)$/)
  if (!match) return null
  try {
    return decodeURIComponent(match[1])
  } catch {
    return match[1]
  }
}

/** Adds the home marker to a destination. Idempotent; keeps its own query. */
export function withHomeParam(to: string, home = getHomeSession()): string {
  if (!home) return to
  const [path, query = ""] = to.split("?")
  const params = new URLSearchParams(query)
  if (params.get(HOME_PARAM) === home) return to
  params.set(HOME_PARAM, home)
  return `${path}?${params.toString()}`
}

/** The home session for this tab: URL first, stored copy as fallback. */
export function getHomeSession(): string | null {
  const fromUrl = new URLSearchParams(window.location.search).get(HOME_PARAM)
  return fromUrl || readStored()
}

/**
 * Resolves this tab's home session once, before first render, and guarantees
 * the URL carries it.
 *
 * Three ways a tab becomes homed:
 *  - it landed with the parameter (`handler ui-open <session>`);
 *  - it already knows its home from storage but arrived without the parameter
 *    (a navigation that dropped it) — the parameter is put back;
 *  - it landed cold on a session detail page (a typed or bookmarked URL) —
 *    that session is adopted as the home.
 */
export function captureHomeSession(): string | null {
  const fromUrl = new URLSearchParams(window.location.search).get(HOME_PARAM)
  if (fromUrl) {
    writeStored(fromUrl)
    return fromUrl
  }

  const home = readStored() ?? sessionIdFromPath(window.location.pathname)
  if (!home) return null

  writeStored(home)
  window.history.replaceState(
    null,
    "",
    withHomeParam(window.location.pathname + window.location.search, home)
  )
  return home
}

/**
 * The banner is hidden only on the home session's own detail page — the page
 * it would send you to. It shows on the listing and on other sessions.
 */
export function shouldShowHomeBanner(home: string | null, pathname: string): boolean {
  if (!home) return false
  return sessionIdFromPath(pathname) !== home
}
