package archive

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/ppiankov/logtap/internal/recv"
)

// Speed controls replay rate. 0 = instant, 1 = realtime, N = fast-forward.
type Speed float64

const (
	SpeedInstant  Speed = 0
	SpeedRealtime Speed = 1
)

// Feeder pushes entries from a Reader into a LogRing at controlled speed.
type Feeder struct {
	reader *Reader
	ring   *recv.LogRing
	filter *Filter

	mu          sync.Mutex
	speed       Speed
	paused      bool
	pauseStart  time.Time
	replayStart time.Time
	firstTS     time.Time
	lastOffset  time.Duration

	stopCh  chan struct{}
	wakeCh  chan struct{} // signaled on speed change or unpause
	wg      sync.WaitGroup
	started bool

	linesEmitted atomic.Int64
	done         atomic.Bool
	scanErr      atomic.Value // stores error
}

// NewFeeder creates a feeder wired to the given reader, ring, and filter.
func NewFeeder(reader *Reader, ring *recv.LogRing, filter *Filter, speed Speed) *Feeder {
	return &Feeder{
		reader: reader,
		ring:   ring,
		filter: filter,
		speed:  speed,
		stopCh: make(chan struct{}),
		wakeCh: make(chan struct{}, 1),
	}
}

// Start launches the feeder goroutine.
func (f *Feeder) Start() {
	f.mu.Lock()
	if f.started {
		f.mu.Unlock()
		return
	}
	f.started = true
	f.mu.Unlock()

	f.wg.Add(1)
	go f.run()
}

// Stop cancels the feeder and waits for completion.
func (f *Feeder) Stop() {
	select {
	case <-f.stopCh:
	default:
		close(f.stopCh)
	}
	f.wg.Wait()
}

// SetSpeed changes the replay speed, recalculating the timeline anchor.
func (f *Feeder) SetSpeed(s Speed) {
	f.mu.Lock()
	f.speed = s
	// recalculate replayStart for seamless transition
	if !f.replayStart.IsZero() && !f.paused {
		now := time.Now()
		f.replayStart = now.Add(-time.Duration(float64(f.lastOffset) / float64(s)))
	}
	f.mu.Unlock()
	f.wake()
}

// Speed returns current replay speed.
func (f *Feeder) Speed() Speed {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.speed
}

// TogglePause toggles pause state. Returns new paused state.
func (f *Feeder) TogglePause() bool {
	f.mu.Lock()
	f.paused = !f.paused
	if f.paused {
		f.pauseStart = time.Now()
	} else {
		// adjust replayStart by pause duration
		if !f.pauseStart.IsZero() {
			pauseDur := time.Since(f.pauseStart)
			f.replayStart = f.replayStart.Add(pauseDur)
			f.pauseStart = time.Time{}
		}
	}
	paused := f.paused
	f.mu.Unlock()
	if !paused {
		f.wake()
	}
	return paused
}

// Paused returns whether the feeder is paused.
func (f *Feeder) Paused() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.paused
}

// LinesEmitted returns the count of entries pushed to the ring.
func (f *Feeder) LinesEmitted() int64 {
	return f.linesEmitted.Load()
}

// Done returns true when scanning has completed.
func (f *Feeder) Done() bool {
	return f.done.Load()
}

// Err returns any error from scanning.
func (f *Feeder) Err() error {
	v := f.scanErr.Load()
	if v == nil {
		return nil
	}
	return v.(error)
}

func (f *Feeder) wake() {
	select {
	case f.wakeCh <- struct{}{}:
	default:
	}
}

func (f *Feeder) run() {
	defer f.wg.Done()
	defer f.done.Store(true)

	_, err := f.reader.Scan(f.filter, func(e recv.LogEntry) bool {
		// check stop
		select {
		case <-f.stopCh:
			return false
		default:
		}

		f.mu.Lock()
		speed := f.speed
		paused := f.paused

		// initialize timeline on first entry
		if f.firstTS.IsZero() {
			f.firstTS = e.Timestamp
			f.replayStart = time.Now()
		}

		entryOffset := e.Timestamp.Sub(f.firstTS)
		f.lastOffset = entryOffset
		f.mu.Unlock()

		// wait while paused
		for paused {
			select {
			case <-f.stopCh:
				return false
			case <-f.wakeCh:
			}
			f.mu.Lock()
			paused = f.paused
			speed = f.speed
			f.mu.Unlock()
		}

		// speed-controlled delay
		if speed > 0 {
			f.mu.Lock()
			targetReal := f.replayStart.Add(time.Duration(float64(entryOffset) / float64(speed)))
			f.mu.Unlock()

			delay := time.Until(targetReal)
			if delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-f.stopCh:
					timer.Stop()
					return false
				case <-f.wakeCh:
					timer.Stop()
					// speed changed or unpaused — recalculate
					f.mu.Lock()
					speed = f.speed
					if speed > 0 {
						targetReal = f.replayStart.Add(time.Duration(float64(entryOffset) / float64(speed)))
					}
					f.mu.Unlock()
					if speed > 0 {
						delay = time.Until(targetReal)
						if delay > 0 {
							time.Sleep(delay)
						}
					}
				case <-timer.C:
				}
			}
		}

		f.ring.Push(e)
		f.linesEmitted.Add(1)
		return true
	})

	if err != nil {
		f.scanErr.Store(err)
	}
}
