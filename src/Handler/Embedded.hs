{-# LANGUAGE TemplateHaskell #-}
-- | Port of embedded.go: skills, hooks, and rules baked into the binary.
-- The Go go:embed globs (skills\/*\/SKILL.md, hooks\/*.sh, rules\/*.md) are
-- mirrored by filtering the embedded directory listings.
module Handler.Embedded
  ( embeddedSkills
  , embeddedHooks
  , embeddedRules
  ) where

import qualified Data.ByteString as BS
import Data.FileEmbed (embedDir)
import Data.List (isSuffixOf)
import System.FilePath (splitDirectories)

-- | Embedded skills as (\"\<name\>\/SKILL.md\", contents) pairs.
embeddedSkills :: [(FilePath, BS.ByteString)]
embeddedSkills =
  [ e
  | e@(path, _) <- $(embedDir "skills")
  , case splitDirectories path of
      [_, "SKILL.md"] -> True
      _               -> False
  ]

-- | Embedded hook scripts as (\"\<name\>.sh\", contents) pairs.
embeddedHooks :: [(FilePath, BS.ByteString)]
embeddedHooks =
  [ e | e@(path, _) <- $(embedDir "hooks"), ".sh" `isSuffixOf` path ]

-- | Embedded rules files as (\"\<name\>.md\", contents) pairs.
embeddedRules :: [(FilePath, BS.ByteString)]
embeddedRules =
  [ e | e@(path, _) <- $(embedDir "rules"), ".md" `isSuffixOf` path ]
