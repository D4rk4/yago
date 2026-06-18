package infrastructure

import "github.com/nikitakarpei/yacy-rwi-node/yacymodel"

func matchesContentDomain(entry yacymodel.RWIEntry, domain string, strict bool) bool {
	switch domain {
	case "image":
		if strict {
			return hasDocType(entry, yacymodel.DocTypeImage)
		}
		return hasAppearanceFlag(entry, yacymodel.RWIFlagHasImage)
	case "audio":
		if strict {
			return hasDocType(entry, yacymodel.DocTypeAudio)
		}
		return hasAppearanceFlag(entry, yacymodel.RWIFlagHasAudio)
	case "video":
		if strict {
			return hasDocType(entry, yacymodel.DocTypeMovie)
		}
		return hasAppearanceFlag(entry, yacymodel.RWIFlagHasVideo)
	case "app":
		return hasAppearanceFlag(entry, yacymodel.RWIFlagHasApp)
	default:
		return true
	}
}

func hasDocType(entry yacymodel.RWIEntry, want byte) bool {
	got, ok := entry.DocType()
	return ok && got == want
}

func hasAppearanceFlag(entry yacymodel.RWIEntry, bit int) bool {
	flags, err := entry.AppearanceFlags()
	if err != nil {
		return false
	}
	return flags.Get(bit)
}
