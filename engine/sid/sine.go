package sid

import (
	"math"
	"sync"
)

type Sine struct {
	Freq     float64
	Aliquots int
	phase    float64
	mu       sync.Mutex
}

func (s *Sine) Gen(volume, sampleRate float64) float64 {
	samp := 0.0
	for ali := 1; ali <= 1<<(s.Aliquots-1); ali *= 2 {
		// Divide by 2.0, to match amplitude with volume (sine goes into negative)
		samp += math.Sin(2*math.Pi*(s.phase/float64(ali))) * (volume / 2.0 / float64(s.Aliquots))
		_, s.phase = math.Modf(s.phase + s.Freq/sampleRate)
	}

	return samp
}

func (s *Sine) Lock() {
	s.mu.Lock()
}

func (s *Sine) Unlock() {
	s.mu.Unlock()
}