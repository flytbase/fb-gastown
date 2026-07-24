package doctor

import "github.com/steveyegge/gastown/internal/config"

// usesHerdrBackend reports the persisted Town transport. Doctor checks use the
// persisted setting rather than a process override so their findings describe
// the Town's durable configuration.
func usesHerdrBackend(ctx *CheckContext) bool {
	if ctx == nil || ctx.TownRoot == "" {
		return false
	}
	settings, err := config.LoadOrCreateTownSettings(config.TownSettingsPath(ctx.TownRoot))
	if err != nil {
		return false
	}
	return config.SessionBackendFromSettings(settings) == config.SessionBackendHerdr
}

func skippedForHerdr(name string) *CheckResult {
	return &CheckResult{
		Name:    name,
		Status:  StatusOK,
		Message: "Not applicable to the Herdr session backend",
	}
}
