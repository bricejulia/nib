package actionpopup

import "github.com/bricejulia/nib/internal/config"

// entry is one command the action popup can search for and run: an
// InPopup action of one of the scopes the popup was built with.
type entry struct {
	ID, Desc, Scope string
}

// popupEntries lists every InPopup action in scopes, in declared order.
func popupEntries(scopes []config.Scope) []entry {
	var out []entry
	for _, s := range scopes {
		for _, a := range s.Actions {
			if a.InPopup {
				out = append(out, entry{ID: a.ID, Desc: a.Desc, Scope: s.Name})
			}
		}
	}
	return out
}
