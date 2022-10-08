package database

type Subscription struct {
	miner uint64
	nick  string
}

func (s *Subscription) Miner() uint64 {
	return s.miner
}

func (s *Subscription) Nick() string {
	return s.nick
}
