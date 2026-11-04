package firstrun

// BluefinCLISelection separates setup intent from observed installed state.
// A late readiness probe must not overwrite an explicit choice; navigation
// must not repeat a successful application, but a failed application can retry.
type BluefinCLISelection struct {
	Selected bool
	Applied  bool
	chosen   bool
}

func NewBluefinCLISelection() BluefinCLISelection {
	return BluefinCLISelection{Selected: true}
}

func (s *BluefinCLISelection) Observe(disabled, enabled bool) {
	if s.chosen {
		return
	}
	s.Selected = !disabled
	s.Applied = disabled || enabled
}

func (s *BluefinCLISelection) Choose(selected bool) {
	s.chosen = true
	if s.Selected != selected {
		s.Selected = selected
		s.Applied = false
	}
}

func (s *BluefinCLISelection) Apply(enable, disable func() error) error {
	if s.Applied {
		return nil
	}
	apply := disable
	if s.Selected {
		apply = enable
	}
	if err := apply(); err != nil {
		return err
	}
	s.Applied = true
	return nil
}
