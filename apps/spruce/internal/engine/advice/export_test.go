package advice

// TemplateTable lists what the template table names: the merged category IDs with a card of their own, the ID prefixes that make cards, and the automatic rule of every template that has one.
func TemplateTable() (ids, prefixes, rules []string) {
	for id, t := range fixed {
		ids = append(ids, id)
		if t.rule != "" {
			rules = append(rules, t.rule)
		}
	}
	prefixes = []string{artifactPrefix, toolPrefix}
	rules = append(rules, projectsIdle.rule)
	return ids, prefixes, rules
}
