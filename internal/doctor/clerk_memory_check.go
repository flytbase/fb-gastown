package doctor

import (
	"fmt"

	"github.com/steveyegge/gastown/internal/clerk"
)

type ClerkMemoryCheck struct{}

func NewClerkMemoryCheck() *ClerkMemoryCheck { return &ClerkMemoryCheck{} }

func (c *ClerkMemoryCheck) Name() string { return "clerk-memory" }
func (c *ClerkMemoryCheck) Description() string {
	return "Verify private Clerk memory isolation and provider health"
}
func (c *ClerkMemoryCheck) CanFix() bool { return false }
func (c *ClerkMemoryCheck) Fix(_ *CheckContext) error {
	return fmt.Errorf("run gt upgrade to repair Clerk memory dependencies and layout")
}

func (c *ClerkMemoryCheck) Run(ctx *CheckContext) *CheckResult {
	result := &CheckResult{Name: c.Name(), Category: CategoryInfrastructure}
	policy, err := clerk.LoadPolicy(ctx.TownRoot)
	if err != nil {
		result.Status = StatusError
		result.Message = fmt.Sprintf("Cannot load Clerk policy: %v", err)
		return result
	}
	if !policy.Memory.Enabled {
		result.Status = StatusOK
		result.Message = "Private Clerk memory is disabled"
		return result
	}
	health := clerk.HealthMemory(policy.Memory)
	if !health.LayoutOK || !health.ProviderOK {
		result.Status = StatusError
		result.Message = health.Warning
		result.FixHint = "Run gt upgrade to repair the private Clerk memory provider"
		result.Details = []string{
			"Canonical root: " + health.CanonicalRoot,
			"Private GBrain home: " + health.GBrainHome,
		}
		return result
	}
	result.Status = StatusOK
	result.Message = fmt.Sprintf("Private GBrain healthy (%s, shared-write=false)", policy.Memory.SearchMode)
	if ctx.Verbose {
		result.Details = []string{
			"Canonical root: " + health.CanonicalRoot,
			"Private GBrain home: " + health.GBrainHome,
			fmt.Sprintf("Shared read-only: %t", policy.Memory.SharedReadOnly),
		}
	}
	return result
}
