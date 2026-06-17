package constant

// Rule keys: the selection-policy half of a directive, shared by the rule
// compiler today and the format canonicaliser and lint later.
const (
	RuleAllowDowngrade = "allow-downgrade"
	RuleBehind         = "behind"
	RuleConstraint     = "constraint"
	RuleCooldown       = "cooldown"
	RuleExclude        = "exclude"
	RuleInclude        = "include"
	RulePrerelease     = "prerelease"
)
