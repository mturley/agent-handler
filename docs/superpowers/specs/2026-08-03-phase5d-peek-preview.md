# Phase 5d: Web UI — Terminal Peek Preview

## Overview

Add a peek preview to session cards in the web UI. A Peek button (eye icon) appears next to the Switch button on peekable sessions. Hovering it shows the last 15-20 lines of cached terminal output in a popover. Clicking it opens a scrollable modal with the full capture and a Switch button in the footer.

---

## UI Components

### Peek Button

- Lucide-react `Eye` icon button, positioned to the left of the Switch button on each session card
- Only rendered for peekable sessions (`session.peekable === true`)
- Tooltip on hover shows a monospace preview of the last 15-20 lines of the cached terminal capture

### Peek Popover (hover)

- Triggered on hover of the Peek button
- Shows the last 15-20 lines of the peek content in a dark monospace container
- Max width ~500px, auto-height
- Plain text in a `<pre>` block — no ANSI conversion needed (cmux capture-pane returns plain text)
- If no peek data is cached, show "No peek data available"

### Peek Modal (click)

- Full cached terminal output in a scrollable monospace container
- Dark background matching the terminal aesthetic
- Header: session name + "Terminal Preview"
- Body: scrollable `<pre>` with the full peek content
- Footer: Switch button (same as session card) + Close button
- The Switch button triggers cmux switch and shows a toast, same as everywhere else

### Data Loading

- Peek content is fetched lazily — only when the user hovers/clicks the Peek button
- Uses `GET /api/sessions/:id/peek` (already exists)
- Cache the response in TanStack Query with a short stale time (10-15s) so it refreshes but doesn't refetch on every hover

---

## Implementation

### Files to Create/Modify

**New:**
- `ui/src/components/PeekPreview.tsx` — the Peek button + popover + modal component

**Modified:**
- `ui/src/components/SessionCard.tsx` — add PeekPreview button next to Switch
- `ui/src/api/client.ts` — verify `getSessionPeek()` exists (it should from Phase 5a)

### No Backend Changes Needed

The `GET /api/sessions/:id/peek` endpoint already exists and returns cached peek_state content. The peek cache (`peek_state` table) is already populated by the statusline hook. No new API work required.
