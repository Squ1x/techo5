package luna

import (
	"encoding/binary"
	"log/slog"
	"math"
	"time"
)

type uplinkStats struct {
	bytes, dropped uint64
	squares        float64
	logged         bool
}

func (s *uplinkStats) add(b []byte) {
	s.bytes += uint64(len(b))
	for i := 0; i < len(b); i += 2 {
		v := float64(int16(binary.LittleEndian.Uint16(b[i:])))
		s.squares += v * v
	}
}

// One numeric summary per turn at endpointing (or cancellation), at most four/s.
// RMS uses signed-16 full scale; no samples or recognized text leave this counter.
func (c *Client) logUplinkLocked() {
	s := &c.uplink
	if s.bytes == 0 || s.logged || time.Since(c.lastUplinkLog) < 250*time.Millisecond {
		return
	}
	c.lastUplinkLog = time.Now()
	s.logged = true
	slog.Info("Luna uplink", "turn", c.turn, "bytes", s.bytes, "rms_s16", math.Round(math.Sqrt(s.squares/float64(s.bytes/2))), "dropped_frames", s.dropped)
}
