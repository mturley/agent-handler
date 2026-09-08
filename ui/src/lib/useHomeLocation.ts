import { useBrowserLocation } from "wouter/use-browser-location"
import { withHomeParam } from "./homeSession"

/**
 * A wouter location hook that re-attaches the home marker to every in-app
 * navigation, so it is present on every URL the tab ever has — the only thing
 * a restored pane gets back.
 *
 * Mounted once at the Router rather than at each navigate() call: no call site
 * can forget, no future page has to remember, and <Link> clicks route through
 * the same hook. wouter matches on pathname alone, so the extra query
 * parameter cannot affect routing.
 */
export function useHomeLocation(): ReturnType<typeof useBrowserLocation> {
  const [location, navigate] = useBrowserLocation()
  const navigateKeepingHome: typeof navigate = (to, options) =>
    navigate(typeof to === "string" ? withHomeParam(to) : to, options)
  return [location, navigateKeepingHome]
}
