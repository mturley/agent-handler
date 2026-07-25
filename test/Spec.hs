module Main (main) where

import Test.Hspec

import qualified ConfigSpec
import qualified Db.CostSpec
import qualified Db.CursorsSpec
import qualified Db.DbSpec
import qualified Db.EventsSpec
import qualified Db.PeekSpec
import qualified Db.ResourceStateSpec
import qualified Db.ResourcesSpec
import qualified Db.SessionsSpec
import qualified Db.SubscriptionsSpec
import qualified FrameworkSpec
import qualified TerminalSpec
import qualified ValidateSpec
import qualified WorktreeSpec

main :: IO ()
main = hspec $ do
  describe "db"                 Db.DbSpec.spec
  describe "db/events"          Db.EventsSpec.spec
  describe "db/sessions"        Db.SessionsSpec.spec
  describe "db/cursors"         Db.CursorsSpec.spec
  describe "db/subscriptions"   Db.SubscriptionsSpec.spec
  describe "db/resources"       Db.ResourcesSpec.spec
  describe "db/resource_state"  Db.ResourceStateSpec.spec
  describe "db/cost"            Db.CostSpec.spec
  describe "db/peek"            Db.PeekSpec.spec
  describe "config"             ConfigSpec.spec
  describe "config/validate"    ValidateSpec.spec
  describe "worktree"           WorktreeSpec.spec
  describe "watcher/framework"  FrameworkSpec.spec
  describe "terminal"           TerminalSpec.spec
