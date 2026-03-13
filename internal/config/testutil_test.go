package config

// explicitAgents returns only non-implicit agents from the slice.
func explicitAgents(agents []Agent) []Agent {
	var out []Agent
	for _, a := range agents {
		if !a.Implicit {
			out = append(out, a)
		}
	}
	return out
}
