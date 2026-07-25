-- | Port of terminal/terminal_test.go.
module TerminalSpec (spec) where

import System.Environment (setEnv, unsetEnv)
import Test.Hspec

import Handler.Terminal (detect, newBackend)

spec :: Spec
spec = do
  it "TestDetectCmux" $ do
    setEnv "CMUX_SURFACE_ID" "test-surface-uuid"
    setEnv "CMUX_WORKSPACE_ID" "test-workspace-uuid"
    unsetEnv "TMUX"
    result <- detect
    result `shouldBe` ("cmux", "test-surface-uuid", "test-workspace-uuid")
    unsetEnv "CMUX_SURFACE_ID"
    unsetEnv "CMUX_WORKSPACE_ID"

  it "TestDetectTmux: tmux or empty when tmux unavailable" $ do
    unsetEnv "CMUX_SURFACE_ID"
    setEnv "TMUX" "/tmp/tmux-501/default,12345,0"
    (backendType, _, _) <- detect
    backendType `shouldSatisfy` (`elem` ["", "tmux"])
    unsetEnv "TMUX"

  it "TestDetectNone" $ do
    unsetEnv "CMUX_SURFACE_ID"
    unsetEnv "TMUX"
    result <- detect
    result `shouldBe` ("", "", "")

  it "TestDetectCmuxPriority" $ do
    setEnv "CMUX_SURFACE_ID" "test-surface-uuid"
    setEnv "TMUX" "/tmp/tmux-501/default,12345,0"
    (backendType, _, _) <- detect
    backendType `shouldBe` "cmux"
    unsetEnv "CMUX_SURFACE_ID"
    unsetEnv "TMUX"

  describe "TestNewBackend" $ do
    it "cmux" $ either (const False) (const True) (newBackend "cmux") `shouldBe` True
    it "tmux" $ either (const False) (const True) (newBackend "tmux") `shouldBe` True
    it "unknown" $ either (const True) (const False) (newBackend "unknown") `shouldBe` True
    it "empty" $ either (const True) (const False) (newBackend "") `shouldBe` True
