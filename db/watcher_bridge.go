package db

import (
	"strings"
	"time"

	watcher "github.com/mturley/watcher"
	wdb "github.com/mturley/watcher/db"
)

// sessionLeaseTTL is how long a session's watcher subscription leases last
// before expiry. Renewed on the session heartbeat; long enough to survive a
// closed laptop over a weekend.
const sessionLeaseTTL = 5 * 24 * time.Hour

const subscriberPrefix = "handler:session:"

// handlerSubscriber is the opaque watcher-library subscriber string handler
// uses for a session. The session id is embedded and recovered by
// sessionIDFromSubscriber; the library never interprets it.
func handlerSubscriber(sessionID string) string { return subscriberPrefix + sessionID }

// handlerSubscriberPrefix is the prefix matching all handler session
// subscriptions, for prefix renew/revoke/list operations.
func handlerSubscriberPrefix() string { return subscriberPrefix }

// sessionIDFromSubscriber recovers the session id from a handler subscriber
// string, or returns ("", false) if the string is not a handler subscriber.
func sessionIDFromSubscriber(subscriber string) (string, bool) {
	if !strings.HasPrefix(subscriber, subscriberPrefix) {
		return "", false
	}
	return strings.TrimPrefix(subscriber, subscriberPrefix), true
}

// subFromWatcher maps a watcher-library subscription row into handler's
// Subscription shape for the given session id. The caller supplies the
// session id explicitly (rather than deriving it here) so it can be used
// both when the session id is already known (ListSubscriptions) and when
// it must first be recovered from the subscriber string (SessionsForResource).
func subFromWatcher(sessionID string, r wdb.Subscription) Subscription {
	var url *string
	if r.Resource.URL != "" {
		u := r.Resource.URL
		url = &u
	}
	return Subscription{
		ID:           r.ID,
		SessionID:    sessionID,
		ResourceType: r.Resource.Type,
		ResourceID:   r.Resource.ID,
		ResourceURL:  url,
		CreatedAt:    r.CreatedAt,
		DeletedAt:    r.DeletedAt,
	}
}

// isLiveSubscription reports whether a watcher-library subscription row is
// currently active: not soft-deleted and not lease-expired. SubscribersOf
// returns rows in any state, so callers that need only live subscribers
// (e.g. FindRelatedSessions) must filter with this.
func isLiveSubscription(r wdb.Subscription) bool {
	if r.DeletedAt != nil {
		return false
	}
	if r.ExpiresAt != nil && *r.ExpiresAt <= time.Now().UTC().Format(time.RFC3339) {
		return false
	}
	return true
}

// eventFromWatcher maps a watcher-library event into handler's Event shape.
// Handler-only fields (SessionID, Broadcast) have no equivalent in the
// library's event model and are left zero-valued.
func eventFromWatcher(e watcher.Event) Event {
	return Event{
		ID:         e.ID,
		TS:         e.TS,
		ExternalTS: e.ExternalTS,
		Source:     e.Source,
		Type:       string(e.Type),
		Title:      e.Title,
		Body:       e.Body,
		Author:     e.Author,
		AuthorType: e.AuthorType,
		Tags:       e.Tags,
	}
}
