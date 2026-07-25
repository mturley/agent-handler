-- | Port of git/status.go: git status gathering for the statusline.
module Handler.Git
  ( GitStatus(..)
  , emptyGitStatus
  , getStatus
  ) where

import Control.Concurrent.Async (concurrently, runConcurrently, Concurrently(..))
import Control.Exception (IOException, try)
import Data.Char (isDigit)
import Data.Maybe (fromMaybe, listToMaybe)
import Data.Text (Text)
import qualified Data.Text as T
import System.Exit (ExitCode(..))
import System.FilePath (isAbsolute, (</>))
import System.Process (readProcessWithExitCode)
import Text.Read (readMaybe)

data GitStatus = GitStatus
  { inGit           :: Bool
  , branch          :: Text
  , defaultBranch   :: Text
  , ahead           :: Int
  , behind          :: Int
  , committedAdds   :: Int
  , committedDels   :: Int
  , modified        :: Int
  , untracked       :: Int
  , uncommittedAdds :: Int
  , uncommittedDels :: Int
  , rebasing        :: Bool
  , rebaseBranch    :: Text
  } deriving (Show, Eq)

emptyGitStatus :: GitStatus
emptyGitStatus = GitStatus False "" "main" 0 0 0 0 0 0 0 0 False ""

gitCmd :: FilePath -> [String] -> IO Text
gitCmd cwd args = do
  result <- try (readProcessWithExitCode "git" (["-C", cwd] ++ args) "")
  pure $ case result :: Either IOException (ExitCode, String, String) of
    Right (ExitSuccess, out, _) -> T.strip (T.pack out)
    _ -> ""

-- | Pulls \"N insertion\" / \"N deletion\" counts out of git --shortstat output.
parseShortstat :: Text -> (Int, Int)
parseShortstat s = (grab " insertion", grab " deletion")
  where
    grab suffix = fromMaybe 0 $ do
      (before, _) <- listToMaybe [T.breakOn suffix s | suffix `T.isInfixOf` s]
      readMaybe (T.unpack (T.takeWhileEnd isDigit before))

-- | Gathers git status for the given working directory.
getStatus :: FilePath -> IO GitStatus
getStatus cwd = do
  (checkCode, _, _) <- readProcessWithExitCode "git" ["-C", cwd, "rev-parse", "--git-dir"] ""
  if checkCode /= ExitSuccess
    then pure emptyGitStatus
    else do
      -- Phase 1: independent lookups, in parallel like the Go version
      ((branch0, defaultRaw), (porcelain, uncommittedStat)) <-
        concurrently
          (concurrently (gitCmd cwd ["rev-parse", "--abbrev-ref", "HEAD"])
                        (gitCmd cwd ["symbolic-ref", "refs/remotes/origin/HEAD"]))
          (concurrently (gitCmd cwd ["status", "--porcelain"])
                        (gitCmd cwd ["diff", "HEAD", "--shortstat"]))

      let defaultBranch = if T.null defaultRaw
            then "main"
            else fromMaybe defaultRaw (T.stripPrefix "refs/remotes/origin/" defaultRaw)
          porcelainLines = filter (not . T.null) (T.lines porcelain)
          untracked = length (filter ("??" `T.isPrefixOf`) porcelainLines)
          modified = length porcelainLines - untracked
          (uAdds, uDels) = parseShortstat uncommittedStat

      -- Detect rebase
      gitDir0 <- gitCmd cwd ["rev-parse", "--git-dir"]
      (rebasing, rebaseBranch) <-
        if T.null gitDir0
          then pure (False, "")
          else do
            let gitDir = if isAbsolute (T.unpack gitDir0)
                           then T.unpack gitDir0
                           else cwd </> T.unpack gitDir0
                readHead d = do
                  r <- try (readFile (gitDir </> d </> "head-name"))
                        :: IO (Either IOException String)
                  pure $ case r of
                    Right contents ->
                      let t = T.strip (T.pack contents)
                      in Just (fromMaybe t (T.stripPrefix "refs/heads/" t))
                    Left _ -> Nothing
            mm <- readHead "rebase-merge"
            ma <- maybe (readHead "rebase-apply") (pure . Just) mm
            pure $ case ma of
              Just b  -> (True, b)
              Nothing -> (False, "")

      let branch = if branch0 == "HEAD" && rebasing then rebaseBranch else branch0

      -- Phase 2: merge-base dependent lookups
      (ahead, behind, cAdds, cDels) <-
        if branch /= "" && branch /= defaultBranch
          then do
            let candidates = ["upstream/" <> defaultBranch, "origin/" <> defaultBranch]
            verified <- mapM (\c -> (,) c <$> gitCmd cwd ["rev-parse", "--verify", T.unpack c]) candidates
            let baseRef = case [c | (c, out) <- verified, not (T.null out)] of
                  (c : _) -> c
                  []      -> defaultBranch
            mergeBase <- gitCmd cwd ["merge-base", T.unpack baseRef, "HEAD"]
            if T.null mergeBase
              then pure (0, 0, 0, 0)
              else do
                (aheadStr, (behindStr, diffStat)) <- runConcurrently $ (,)
                  <$> Concurrently (gitCmd cwd ["rev-list", "--count", T.unpack mergeBase <> "..HEAD"])
                  <*> ((,) <$> Concurrently (gitCmd cwd ["rev-list", "--count", "HEAD.." <> T.unpack baseRef])
                           <*> Concurrently (gitCmd cwd ["diff", "--shortstat", T.unpack mergeBase <> "..HEAD"]))
                let (a, d) = parseShortstat diffStat
                pure ( fromMaybe 0 (readMaybe (T.unpack aheadStr))
                     , fromMaybe 0 (readMaybe (T.unpack behindStr))
                     , a, d )
          else pure (0, 0, 0, 0)

      pure GitStatus
        { inGit = True
        , branch = branch
        , defaultBranch = defaultBranch
        , ahead = ahead
        , behind = behind
        , committedAdds = cAdds
        , committedDels = cDels
        , modified = modified
        , untracked = untracked
        , uncommittedAdds = uAdds
        , uncommittedDels = uDels
        , rebasing = rebasing
        , rebaseBranch = rebaseBranch
        }
