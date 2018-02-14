package types

import "sort"

// SortFlags of the virtual service in place.
func (s *VirtualService) SortFlags() {
	if s.Config != nil && len(s.Config.Flags) > 0 {
		// flags are expected to be in order to compare against local state
		sort.Strings(s.Config.Flags)
	}
}
