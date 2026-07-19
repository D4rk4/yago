package crawlbroker

func (r *ControlRegistry) bindFleetFetchStarts(
	setPagesPerSecond func(uint32) error,
) {
	r.processRateUpdate.Lock()
	defer r.processRateUpdate.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setFleetPagesPerSecond = setPagesPerSecond
}

func (s *exchangeServer) setFleetPagesPerSecond(pagesPerSecond uint32) error {
	s.fetchPolicy.Lock()
	defer s.fetchPolicy.Unlock()
	if s.fetchStarts == nil {
		return errFleetFetchPolicyInvalid
	}
	previousPagesPerSecond := s.fetchStarts.Snapshot().PagesPerSecond
	if err := s.fetchStarts.SetPagesPerSecond(pagesPerSecond); err != nil {
		return err
	}
	if pagesPerSecond > 0 && (previousPagesPerSecond == 0 ||
		previousPagesPerSecond > pagesPerSecond) {
		s.sessions.disconnectActiveSessions()
	} else if pagesPerSecond > 0 {
		s.sessions.disconnectWithoutFetchStartLeases()
	}

	return nil
}

func (s *exchangeServer) bindControl(control *ControlRegistry) error {
	if control == nil {
		return errFleetFetchPolicyInvalid
	}
	control.processRateUpdate.Lock()
	defer control.processRateUpdate.Unlock()
	control.mu.Lock()
	defer control.mu.Unlock()
	s.fetchPolicy.Lock()
	defer s.fetchPolicy.Unlock()
	if s.fetchStarts == nil {
		return errFleetFetchPolicyInvalid
	}
	if err := s.fetchStarts.SetPagesPerSecond(control.processPagesPerSecond); err != nil {
		return err
	}
	s.control = control
	control.setFleetPagesPerSecond = s.setFleetPagesPerSecond

	return nil
}
