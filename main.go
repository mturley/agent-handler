package main

import "github.com/mturley/agent-handler/cmd"

func main() {
	cmd.SetEmbedded(EmbeddedSkills, EmbeddedHooks, EmbeddedRules)
	cmd.Execute()
}
