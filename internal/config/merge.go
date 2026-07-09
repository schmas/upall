package config

// Merge folds step definitions in layer order (embedded first, then user) into
// one def per key. A later def overrides ONLY the fields it sets: nil pointers
// and nil slices inherit from the accumulated lower layer, so setting just
// `order` in a user file keeps the default `run`, `sudo`, etc. The Env map
// merges key-wise rather than replacing wholesale.
func Merge(defs []StepDef) []StepDef {
	order := make([]string, 0, len(defs))
	byKey := make(map[string]StepDef, len(defs))
	for _, d := range defs {
		if base, ok := byKey[d.Key]; ok {
			byKey[d.Key] = mergeInto(base, d)
			continue
		}
		order = append(order, d.Key)
		byKey[d.Key] = d
	}
	out := make([]StepDef, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	return out
}

// mergeInto returns base overlaid with the set fields of over (the higher layer).
func mergeInto(base, over StepDef) StepDef {
	if over.Label != nil {
		base.Label = over.Label
	}
	if over.OS != nil {
		base.OS = over.OS
	}
	if over.Distro != nil {
		base.Distro = over.Distro
	}
	if over.Detect != nil {
		base.Detect = over.Detect
	}
	if over.Shell != nil {
		base.Shell = over.Shell
	}
	if over.Sudo != nil {
		base.Sudo = over.Sudo
	}
	if over.Run != nil {
		base.Run = over.Run
	}
	if over.Enabled != nil {
		base.Enabled = over.Enabled
	}
	if over.Order != nil {
		base.Order = over.Order
	}
	if over.Timeout != nil {
		base.Timeout = over.Timeout
	}
	if over.Env != nil {
		merged := make(map[string]string, len(base.Env)+len(over.Env))
		for k, v := range base.Env {
			merged[k] = v
		}
		for k, v := range over.Env {
			merged[k] = v
		}
		base.Env = merged
	}
	return base
}
