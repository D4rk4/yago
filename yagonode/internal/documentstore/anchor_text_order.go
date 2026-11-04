package documentstore

type anchorTextIdentity struct {
	url           string
	text          string
	noFollow      bool
	userGenerated bool
	sponsored     bool
}

func anchorTextLess(left, right AnchorText) bool {
	if left.URL != right.URL {
		return left.URL < right.URL
	}
	if left.NoFollow != right.NoFollow {
		return !left.NoFollow
	}
	if left.UserGenerated != right.UserGenerated {
		return !left.UserGenerated
	}
	if left.Sponsored != right.Sponsored {
		return !left.Sponsored
	}
	return left.Text < right.Text
}
