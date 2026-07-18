package frontier

func (f *Frontier) scheduleSettlement(settle func(bool), succeeded bool) {
	f.settlements.Add(1)
	go func() {
		defer f.settlements.Done()
		settle(succeeded)
	}()
}

func (f *Frontier) scheduleSettlements(finishes []runFinish) {
	for _, completion := range finishes {
		f.scheduleSettlement(completion.finish, completion.succeeded)
	}
}

func (f *Frontier) WaitForSettlements() {
	f.settlements.Wait()
}
