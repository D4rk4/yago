package infrastructure

import "github.com/nikitakarpei/yacy-rwi-node/yacymodel"

func matchesConstraint(entry yacymodel.RWIEntry, constraint string) bool {
	if constraint == "" {
		return true
	}
	required, err := yacymodel.DecodeBitfield(constraint)
	if err != nil || required.AllSet(yacymodel.RWIFlagBitCount) {
		return true
	}
	flags, err := entry.AppearanceFlags()
	if err != nil {
		return false
	}
	for bit := range yacymodel.RWIFlagBitCount {
		if required.Get(bit) && flags.Get(bit) {
			return true
		}
	}
	return false
}
