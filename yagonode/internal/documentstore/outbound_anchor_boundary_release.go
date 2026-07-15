package documentstore

func (d documentVault) releaseOwnedDocumentLineage(
	owned bool,
	reservation DocumentLineageReservation,
) {
	if owned {
		d.ReleaseDocumentLineages(reservation)
	}
}

func releaseCurrentOutboundAnchorBoundaries(release *func()) {
	if *release != nil {
		(*release)()
	}
}

func (d documentVault) outboundAnchorBoundaryRelease(
	releaseTargets func(),
	owned bool,
	reservation DocumentLineageReservation,
) func() {
	return func() {
		releaseTargets()
		d.releaseOwnedDocumentLineage(owned, reservation)
	}
}
