package system

import (
	"github.com/spf13/afero"
)

// SameOwner reports whether the link and the target it resolves to belong
// to the same user: the agent, running as root, does not follow a link a
// user made into the files of another one. False when the file system does
// not tell the owners.
func (s *System) SameOwner(link, target string) bool {
	lstater, ok := s.ctx.Fs.(afero.Lstater)
	if !ok {
		return false
	}
	lfi, _, err := lstater.LstatIfPossible(link)
	if err != nil {
		return false
	}
	tfi, err := s.ctx.Fs.Stat(target)
	if err != nil {
		return false
	}
	luid, _, lok := fileOwner(lfi)
	tuid, _, tok := fileOwner(tfi)
	return lok && tok && luid == tuid
}
