-- | Small shared helpers used across the handler codebase.
module Handler.Util
  ( nowIso
  , newUuid
  , epochIso
  , textShow
  ) where

import Data.Text (Text)
import qualified Data.Text as T
import Data.Time.Clock (getCurrentTime)
import Data.Time.Format (defaultTimeLocale, formatTime)
import qualified Data.UUID as UUID
import qualified Data.UUID.V4 as UUID

-- | Current time as ISO 8601 UTC, matching Go's time.RFC3339 rendering.
nowIso :: IO Text
nowIso = T.pack . formatTime defaultTimeLocale "%Y-%m-%dT%H:%M:%SZ" <$> getCurrentTime

-- | Random UUIDv4 as lowercase text, matching google/uuid.NewString().
newUuid :: IO Text
newUuid = UUID.toText <$> UUID.nextRandom

-- | The zero cursor used when a session has never seen an event.
epochIso :: Text
epochIso = "1970-01-01T00:00:00Z"

textShow :: Show a => a -> Text
textShow = T.pack . show
