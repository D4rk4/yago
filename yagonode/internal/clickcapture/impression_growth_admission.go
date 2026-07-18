package clickcapture

import "fmt"

type ImpressionGrowthAdmission interface {
	CheckGrowth() error
}

func (s *Store) AdmitImpressionGrowth(admission ImpressionGrowthAdmission) {
	s.impressionGrowth = admission
}

func (s *Store) admitImpressionGrowth() error {
	if s.impressionGrowth == nil {
		return nil
	}
	if err := s.impressionGrowth.CheckGrowth(); err != nil {
		return fmt.Errorf("admit impression growth: %w", err)
	}

	return nil
}
