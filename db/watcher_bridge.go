package db

import (
	"strings"
	"time"
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
