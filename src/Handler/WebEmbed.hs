{-# LANGUAGE TemplateHaskell #-}
-- | Port of web_embed.go: the built React UI (ui/dist) embedded at compile
-- time. When the UI hasn't been built, only .gitkeep is embedded and
-- 'webHasContent' is False (the ui command prints a hint instead of serving).
module Handler.WebEmbed
  ( webFiles
  , webHasContent
  , lookupWebFile
  , webIndexHtml
  , contentTypeFor
  ) where

import qualified Data.ByteString as BS
import Data.FileEmbed (embedDir)
import Data.List (lookup)
import Data.Text (Text)
import qualified Data.Text as T

-- | Every file under ui/dist as (relative path, contents).
webFiles :: [(FilePath, BS.ByteString)]
webFiles = $(embedDir "ui/dist")

-- | Whether the embedded UI has real files (anything besides .gitkeep).
webHasContent :: Bool
webHasContent = any (\(p, _) -> p /= ".gitkeep") webFiles

-- | Looks up an embedded file by its path relative to ui/dist (no leading /).
lookupWebFile :: Text -> Maybe BS.ByteString
lookupWebFile p = lookup (T.unpack p) webFiles

-- | The SPA entry point, if built.
webIndexHtml :: Maybe BS.ByteString
webIndexHtml = lookupWebFile "index.html"

-- | Content type by file extension, standing in for Go's http.FileServer
-- sniffing. Defaults to application/octet-stream.
contentTypeFor :: Text -> Text
contentTypeFor path = case T.takeWhileEnd (/= '.') path of
  "html"  -> "text/html; charset=utf-8"
  "js"    -> "text/javascript; charset=utf-8"
  "mjs"   -> "text/javascript; charset=utf-8"
  "css"   -> "text/css; charset=utf-8"
  "json"  -> "application/json"
  "map"   -> "application/json"
  "svg"   -> "image/svg+xml"
  "png"   -> "image/png"
  "jpg"   -> "image/jpeg"
  "jpeg"  -> "image/jpeg"
  "gif"   -> "image/gif"
  "ico"   -> "image/x-icon"
  "woff"  -> "font/woff"
  "woff2" -> "font/woff2"
  "ttf"   -> "font/ttf"
  "txt"   -> "text/plain; charset=utf-8"
  "wasm"  -> "application/wasm"
  _       -> "application/octet-stream"
